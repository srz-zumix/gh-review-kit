package insights

import (
	"testing"

	"github.com/google/go-github/v93/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// toReviewCommentRecord must inherit the review_state of the review an inline
// comment belongs to, so blocking/review-state filters see inline comments.
func TestToReviewCommentRecordAppliesReviewState(t *testing.T) {
	pr := &github.PullRequest{Number: github.Ptr(7)}
	c := &gh.CopilotAttributedPullRequestComment{PullRequestComment: github.PullRequestComment{
		ID:                  github.Ptr(int64(100)),
		PullRequestReviewID: github.Ptr(int64(42)),
		Body:                github.Ptr("please fix this"),
		User:                &github.User{Login: github.Ptr("alice")},
		Path:                github.Ptr("src/main.go"),
	}}
	reviewStates := map[int64]*gh.CopilotAttributedPullRequestReview{
		42: {PullRequestReview: github.PullRequestReview{State: github.Ptr("CHANGES_REQUESTED")}},
	}

	rec := toReviewCommentRecord("o/r", pr, c, reviewStates, ExtractOptions{})
	if rec == nil {
		t.Fatal("expected a record, got nil")
	}
	if rec.ReviewState != "CHANGES_REQUESTED" {
		t.Fatalf("ReviewState: got %q want CHANGES_REQUESTED", rec.ReviewState)
	}

	// Unknown review ID leaves ReviewState empty (same as before the fix).
	c.PullRequestReviewID = github.Ptr(int64(999))
	rec = toReviewCommentRecord("o/r", pr, c, reviewStates, ExtractOptions{})
	if rec == nil {
		t.Fatal("expected a record, got nil")
	}
	if rec.ReviewState != "" {
		t.Fatalf("ReviewState for unknown review id: got %q want empty", rec.ReviewState)
	}
}

// Copilot attribution on reviews and inline comments must be carried into the
// dataset, with inline comments inheriting the review effort.
func TestCopilotAttributionRecords(t *testing.T) {
	pr := &github.PullRequest{Number: github.Ptr(7)}
	yes := true
	effort := "max"
	review := &gh.CopilotAttributedPullRequestReview{
		PullRequestReview: github.PullRequestReview{
			ID:    github.Ptr(int64(42)),
			Body:  github.Ptr("summary"),
			State: github.Ptr("COMMENTED"),
			User:  &github.User{Login: github.Ptr("example-bot[bot]"), Type: github.Ptr("Bot")},
		},
		IsCopilotCodeReview:     &yes,
		CopilotCodeReviewEffort: &effort,
	}
	opts := ExtractOptions{IncludeBots: true}
	body := toReviewBodyRecord("o/r", pr, review, opts)
	if body == nil || !body.IsCopilotCodeReview || body.CopilotCodeReviewEffort != "max" {
		t.Fatalf("review body record = %+v", body)
	}

	sources := []gh.CopilotCodeReviewSource{{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "example-skill"}}
	c := &gh.CopilotAttributedPullRequestComment{
		PullRequestComment: github.PullRequestComment{
			ID:                  github.Ptr(int64(100)),
			PullRequestReviewID: github.Ptr(int64(42)),
			Body:                github.Ptr("consider this"),
			User:                &github.User{Login: github.Ptr("example-bot[bot]"), Type: github.Ptr("Bot")},
			Path:                github.Ptr("src/main.go"),
		},
		IsCopilotCodeReview:      &yes,
		CopilotCodeReviewSources: sources,
	}
	rec := toReviewCommentRecord("o/r", pr, c, map[int64]*gh.CopilotAttributedPullRequestReview{42: review}, opts)
	if rec == nil || !rec.IsCopilotCodeReview || rec.CopilotCodeReviewEffort != "max" || len(rec.CopilotCodeReviewSources) != 1 {
		t.Fatalf("review comment record = %+v", rec)
	}

	plain := &gh.CopilotAttributedPullRequestReview{PullRequestReview: github.PullRequestReview{
		ID:   github.Ptr(int64(43)),
		Body: github.Ptr("looks good"),
		User: &github.User{Login: github.Ptr("alice")},
	}}
	if rec := toReviewBodyRecord("o/r", pr, plain, opts); rec == nil || rec.IsCopilotCodeReview || rec.CopilotCodeReviewEffort != "" {
		t.Fatalf("plain review record = %+v", rec)
	}
}

func TestCopilotGroupKeys(t *testing.T) {
	sourceFn, err := groupKeyFn("copilot_source")
	if err != nil {
		t.Fatal(err)
	}
	effortFn, err := groupKeyFn("copilot_effort")
	if err != nil {
		t.Fatal(err)
	}
	c := &Comment{
		CopilotCodeReviewEffort: "lite",
		CopilotCodeReviewSources: []gh.CopilotCodeReviewSource{
			{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "example-skill"},
			{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "example-skill"},
			{Kind: gh.CopilotCodeReviewSourceKindCustomInstructions, Name: "instructions", Path: ".github/copilot-instructions.md"},
		},
	}
	got := sourceFn(c, nil)
	want := []string{"SKILL:example-skill", "CUSTOM_INSTRUCTIONS:.github/copilot-instructions.md"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("copilot_source keys = %v, want %v", got, want)
	}
	if got := effortFn(c, nil); len(got) != 1 || got[0] != "lite" {
		t.Errorf("copilot_effort keys = %v", got)
	}
	empty := &Comment{}
	if got := sourceFn(empty, nil); len(got) != 1 || got[0] != "(none)" {
		t.Errorf("copilot_source keys for empty = %v", got)
	}
	if got := effortFn(empty, nil); len(got) != 1 || got[0] != "(none)" {
		t.Errorf("copilot_effort keys for empty = %v", got)
	}
}
