package copilot

import "testing"

func TestParseEvaluator(t *testing.T) {
	tests := []struct {
		name    string
		want    Evaluator
		wantErr bool
	}{
		{name: "", want: EvaluatorCopilot},
		{name: "copilot", want: EvaluatorCopilot},
		{name: "claude", want: EvaluatorClaude},
		{name: "false", wantErr: true},
		{name: "Claude", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEvaluator(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseEvaluator(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseEvaluator(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
