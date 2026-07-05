package comments

import (
	"testing"

	"github.com/google/go-github/v84/github"
)

// toReviewCommentRecord must inherit the review_state of the review an inline
// comment belongs to, so blocking/review-state filters see inline comments.
func TestToReviewCommentRecordAppliesReviewState(t *testing.T) {
	pr := &github.PullRequest{Number: github.Ptr(7)}
	c := &github.PullRequestComment{
		ID:                  github.Ptr(int64(100)),
		PullRequestReviewID: github.Ptr(int64(42)),
		Body:                github.Ptr("please fix this"),
		User:                &github.User{Login: github.Ptr("alice")},
		Path:                github.Ptr("src/main.go"),
	}
	reviewStates := map[int64]string{42: "CHANGES_REQUESTED"}

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
