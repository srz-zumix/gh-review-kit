package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v93/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

func TestGetReviewStatusBotRequest(t *testing.T) {
	for _, requested := range []bool{true, false} {
		t.Run(fmt.Sprintf("requested=%t", requested), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/repos/owner/repo/pulls/37/requested_reviewers":
					_, _ = w.Write([]byte(`{"users":[],"teams":[]}`))
				case "/repos/owner/repo/pulls/37":
					_, _ = w.Write([]byte(`{"number":37,"head":{"sha":"latest"}}`))
				case "/repos/owner/repo/pulls/37/reviews":
					_, _ = w.Write([]byte(`[{"state":"COMMENTED","user":{"login":"copilot-pull-request-reviewer[bot]"},"submitted_at":"2026-10-02T15:49:18Z","commit_id":"earlier"}]`))
				case "/api/graphql":
					var request struct {
						Query string `json:"query"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
						return
					}
					if strings.Contains(request.Query, "reviewRequests") {
						nodes := `[]`
						if requested {
							nodes = `[{"requestedReviewer":{"login":"copilot-pull-request-reviewer"}}]`
						}
						_, _ = fmt.Fprintf(w, `{"data":{"repository":{"pullRequest":{"reviewRequests":{"nodes":%s,"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, nodes)
					} else if strings.Contains(request.Query, "reviewThreads") {
						_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`))
					} else {
						t.Errorf("unexpected query: %s", request.Query)
					}
				default:
					t.Errorf("unexpected API path: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			baseURL := server.URL + "/"
			rest, err := github.NewClient(github.WithHTTPClient(server.Client()), github.WithURLs(&baseURL, nil))
			if err != nil {
				t.Fatal(err)
			}
			g, err := client.NewClient(rest)
			if err != nil {
				t.Fatal(err)
			}
			repo := repository.Repository{Owner: "owner", Name: "repo"}
			status, err := GetReviewStatus(context.Background(), g, repo, 37)
			if err != nil {
				t.Fatal(err)
			}
			want := StatusCommented
			if requested {
				want = StatusInProgress
			}
			if status.Status != want || status.Requested != requested {
				t.Errorf("status = %+v, want status %q and requested %t", status, want, requested)
			}
			if status.ReviewCount != 1 || status.LastReviewState != gh.PullRequestReviewStateCommented || status.HeadReviewed {
				t.Errorf("previous review metadata not preserved: %+v", status)
			}
			gotRequested, err := IsCopilotRequested(context.Background(), g, repo, 37)
			if err != nil {
				t.Fatal(err)
			}
			if gotRequested != requested {
				t.Errorf("IsCopilotRequested() = %t, want %t", gotRequested, requested)
			}
		})
	}
}

func TestDeriveStatus(t *testing.T) {
	tests := []struct {
		name            string
		requested       bool
		lastReviewState string
		want            Status
	}{
		{"never requested", false, "", StatusNotRequested},
		{"requested", true, "", StatusInProgress},
		{"re-requested after a review", true, gh.PullRequestReviewStateCommented, StatusInProgress},
		{"commented", false, gh.PullRequestReviewStateCommented, StatusCommented},
		{"approved", false, gh.PullRequestReviewStateApproved, StatusApproved},
		{"changes requested", false, gh.PullRequestReviewStateChangesRequested, StatusChangesRequested},
		{"dismissed", false, gh.PullRequestReviewStateDismissed, StatusDismissed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveStatus(tt.requested, tt.lastReviewState); got != tt.want {
				t.Errorf("deriveStatus(%t, %q) = %q, want %q", tt.requested, tt.lastReviewState, got, tt.want)
			}
		})
	}
}

func TestPullRequestStateStatus(t *testing.T) {
	tests := []struct {
		name   string
		merged bool
		state  string
		want   Status
	}{
		{"open", false, "open", ""},
		{"merged", true, "closed", StatusMerged},
		{"closed", false, "closed", StatusClosed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pullRequestStateStatus(tt.merged, tt.state); got != tt.want {
				t.Errorf("pullRequestStateStatus(%t, %q) = %q, want %q", tt.merged, tt.state, got, tt.want)
			}
		})
	}
}

func TestDecideReviewRequest(t *testing.T) {
	tests := []struct {
		name   string
		status ReviewStatus
		want   bool
	}{
		{"merged pull request", ReviewStatus{Status: StatusMerged, ReviewCount: 1}, false},
		{"closed pull request", ReviewStatus{Status: StatusClosed}, false},
		{"never requested", ReviewStatus{}, true},
		{"reviewed an earlier commit only", ReviewStatus{ReviewCount: 1}, true},
		{"requested and waiting for an answer", ReviewStatus{Requested: true}, false},
		{"re-requested after a review", ReviewStatus{Requested: true, ReviewCount: 1}, false},
		{"latest commit already reviewed", ReviewStatus{ReviewCount: 1, HeadReviewed: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := DecideReviewRequest(&tt.status)
			if got != tt.want {
				t.Errorf("DecideReviewRequest() = %v, want %v", got, tt.want)
			}
			if reason == "" {
				t.Error("DecideReviewRequest() returned an empty reason")
			}
		})
	}
}

func TestIsCopilotLogin(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{DefaultAuthor, true},
		{ReviewerLogin, true},
		{"Copilot-Pull-Request-Reviewer[bot]", true},
		{"Copilot", true},
		{"COPILOT", true},
		{"copilot[bot]", true},
		{"Copilot[BOT]", true},
		{"copilot-swe-agent[bot]", false},
		{"copilot-user", false},
		{"dependabot[bot]", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			if got := isCopilotLogin(tt.login); got != tt.want {
				t.Errorf("isCopilotLogin(%q) = %t, want %t", tt.login, got, tt.want)
			}
		})
	}
}

func TestSummarizeCopilotReviews(t *testing.T) {
	ptr := func(v bool) *bool { return &v }
	effort := func(v string) *string { return &v }
	review := func(login, state, commit, submitted string, isCopilot *bool, eff *string) *gh.CopilotAttributedPullRequestReview {
		ts, err := time.Parse(time.RFC3339, submitted)
		if err != nil {
			t.Fatal(err)
		}
		return &gh.CopilotAttributedPullRequestReview{
			PullRequestReview: github.PullRequestReview{
				User:        &github.User{Login: github.Ptr(login)},
				State:       github.Ptr(state),
				CommitID:    github.Ptr(commit),
				SubmittedAt: &github.Timestamp{Time: ts},
			},
			IsCopilotCodeReview:     isCopilot,
			CopilotCodeReviewEffort: eff,
		}
	}

	reviews := []*gh.CopilotAttributedPullRequestReview{
		// Attributed Copilot review with effort.
		review("copilot-pull-request-reviewer[bot]", "COMMENTED", "old", "2026-10-01T00:00:00Z", ptr(true), effort("lite")),
		// Explicit attribution overrides the login: a human-looking login produced by Copilot.
		review("someone", "COMMENTED", "head", "2026-10-03T00:00:00Z", ptr(true), effort("max")),
		// Explicitly not Copilot despite the bot login.
		review("copilot-pull-request-reviewer[bot]", "APPROVED", "head", "2026-10-04T00:00:00Z", ptr(false), nil),
		// Server without the attribution field falls back to the login.
		review("copilot-pull-request-reviewer[bot]", "COMMENTED", "old", "2026-10-02T00:00:00Z", nil, nil),
		// Pending reviews are skipped.
		review("copilot-pull-request-reviewer[bot]", "PENDING", "head", "2026-10-05T00:00:00Z", ptr(true), effort("balanced")),
	}
	status := &ReviewStatus{}
	summarizeCopilotReviews(status, reviews, "head")

	if status.ReviewCount != 3 {
		t.Errorf("ReviewCount = %d, want 3", status.ReviewCount)
	}
	if status.LastReviewEffort != "max" || status.LastReviewState != "COMMENTED" {
		t.Errorf("last review = %q/%q, want COMMENTED/max", status.LastReviewState, status.LastReviewEffort)
	}
	if !status.HeadReviewed {
		t.Error("HeadReviewed = false, want true")
	}

	// The latest review without an effort clears the previously recorded effort.
	status = &ReviewStatus{}
	summarizeCopilotReviews(status, reviews[:1], "head")
	summarizeCopilotReviews(status, []*gh.CopilotAttributedPullRequestReview{review("copilot", "COMMENTED", "x", "2026-10-09T00:00:00Z", nil, nil)}, "head")
	if status.LastReviewEffort != "" {
		t.Errorf("LastReviewEffort = %q, want empty", status.LastReviewEffort)
	}
}
