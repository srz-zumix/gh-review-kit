package copilot

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v93/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

func TestApplyEvaluationDryRun(t *testing.T) {
	tests := []struct {
		verdict Verdict
		want    Action
	}{
		{VerdictValid, ActionResolved},
		{VerdictInvalid, ActionFeedback},
		{VerdictUnclear, ActionResolved},
		{Verdict("other"), ActionNone},
	}
	for _, tt := range tests {
		t.Run(string(tt.verdict), func(t *testing.T) {
			got, err := ApplyEvaluation(context.Background(), nil, repository.Repository{}, &Comment{}, &Evaluation{Verdict: tt.verdict}, true)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ApplyEvaluation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyEvaluationUnclearResolvesAsWontFix(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			var bodies []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/api/graphql" {
					t.Errorf("unexpected API path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				bodies = append(bodies, string(body))
				if fail {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"message":"boom"}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":{"resolveReviewThread":{"thread":{"id":"T_1","isResolved":true}}}}`))
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
			comment := &Comment{CommentID: 1, ThreadID: "T_1"}
			got, err := ApplyEvaluation(context.Background(), g, repo, comment, &Evaluation{Verdict: VerdictUnclear}, false)
			if fail {
				if err == nil {
					t.Fatal("ApplyEvaluation() error = nil, want error")
				}
				if got != ActionNone {
					t.Errorf("ApplyEvaluation() = %q, want %q", got, ActionNone)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != ActionResolved {
				t.Errorf("ApplyEvaluation() = %q, want %q", got, ActionResolved)
			}
			if len(bodies) != 1 {
				t.Fatalf("GraphQL requests = %d, want 1", len(bodies))
			}
			if !strings.Contains(bodies[0], "WONT_FIX") || !strings.Contains(bodies[0], "T_1") {
				t.Errorf("mutation does not resolve T_1 as WONT_FIX: %s", bodies[0])
			}
		})
	}
}
