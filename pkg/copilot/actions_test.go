package copilot

import (
	"context"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
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
