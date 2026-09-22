package copilot

import (
	"testing"
)

func TestAssignBatchResults(t *testing.T) {
	comments := []*Comment{
		{CommentID: 1},
		{CommentID: 2},
		{CommentID: 3},
	}
	evals := map[int64]*Evaluation{
		1: {Verdict: VerdictValid, Reason: "ok"},
		3: {Verdict: VerdictInvalid, Reason: "no"},
	}

	results := assignBatchResults(comments, evals)
	if len(results) != 3 {
		t.Fatalf("assignBatchResults() returned %d results, want 3", len(results))
	}
	if results[0].Evaluation == nil || results[0].Evaluation.Verdict != VerdictValid {
		t.Errorf("results[0].Evaluation = %+v, want verdict %q", results[0].Evaluation, VerdictValid)
	}
	if results[1].Evaluation != nil || results[1].Error == "" {
		t.Errorf("results[1] = %+v, want no evaluation and a non-empty error", results[1])
	}
	if results[2].Evaluation == nil || results[2].Evaluation.Verdict != VerdictInvalid {
		t.Errorf("results[2].Evaluation = %+v, want verdict %q", results[2].Evaluation, VerdictInvalid)
	}
}
