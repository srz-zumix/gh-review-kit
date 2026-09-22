package copilot

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeCopilotBin writes a shell script standing in for the Copilot CLI that
// prints output regardless of its arguments and exits with exitCode.
func fakeCopilotBin(t *testing.T, output string, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "copilot")
	script := "#!/bin/sh\ncat <<'EOF'\n" + output + "\nEOF\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake copilot script: %v", err)
	}
	return path
}

func TestBuildArgs(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	tests := []struct {
		name string
		opts EvaluateOptions
		want []string
	}{
		{
			name: "minimal",
			opts: EvaluateOptions{},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none"},
		},
		{
			name: "agent and session id",
			opts: EvaluateOptions{Agent: "reviewer", SessionID: "sess-1"},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--agent", "reviewer", "--session-id", "sess-1"},
		},
		{
			name: "extra args appended last",
			opts: EvaluateOptions{ExtraArgs: []string{"--allow-all-tools", "--allow-tool", "shell(git:*)"}},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--allow-all-tools", "--allow-tool", "shell(git:*)"},
		},
		{
			name: "sandbox",
			opts: EvaluateOptions{Sandbox: true},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--experimental", "--sandbox", "--add-dir", wd},
		},
		{
			name: "sandbox before extra args",
			opts: EvaluateOptions{Sandbox: true, ExtraArgs: []string{"--allow-tool", "shell(git:*)"}},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--experimental", "--sandbox", "--add-dir", wd, "--allow-tool", "shell(git:*)"},
		},
		{
			name: "allow all tools",
			opts: EvaluateOptions{AllowAllTools: true},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--allow-all-tools"},
		},
		{
			name: "model",
			opts: EvaluateOptions{Model: "gpt-5"},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--model", "gpt-5"},
		},
		{
			name: "model and allow all tools before extra args",
			opts: EvaluateOptions{Model: "gpt-5", AllowAllTools: true, ExtraArgs: []string{"--deny-tool", "shell(rm:*)"}},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none", "--model", "gpt-5", "--allow-all-tools", "--deny-tool", "shell(rm:*)"},
		},
		{
			name: "rubber duck does not change args",
			opts: EvaluateOptions{RubberDuck: true},
			want: []string{"-p", "prompt", "--no-color", "--log-level", "none"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildArgs(tt.opts, "prompt")
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewSessionID(t *testing.T) {
	first := NewSessionID()
	if first == NewSessionID() {
		t.Errorf("NewSessionID() = %q for two calls, want distinct sessions", first)
	}
}

func TestParseEvaluationFencedJSON(t *testing.T) {
	output := "Some reasoning...\n\n```json\n{\"verdict\": \"valid\", \"reason\": \"looks correct\"}\n```\n"
	eval, err := parseEvaluation(output)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Verdict != VerdictValid {
		t.Errorf("Verdict = %q, want %q", eval.Verdict, VerdictValid)
	}
	if eval.Reason != "looks correct" {
		t.Errorf("Reason = %q, want %q", eval.Reason, "looks correct")
	}
}

func TestParseEvaluationBareJSON(t *testing.T) {
	output := "{\"verdict\": \"invalid\", \"reason\": \"false positive\"}"
	eval, err := parseEvaluation(output)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Verdict != VerdictInvalid {
		t.Errorf("Verdict = %q, want %q", eval.Verdict, VerdictInvalid)
	}
}

func TestParseEvaluationLastFencedBlockWins(t *testing.T) {
	output := "```json\n{\"verdict\": \"unclear\", \"reason\": \"draft\"}\n```\nmore output\n```json\n{\"verdict\": \"valid\", \"reason\": \"final\"}\n```\n"
	eval, err := parseEvaluation(output)
	if err != nil {
		t.Fatalf("parseEvaluation() error = %v", err)
	}
	if eval.Verdict != VerdictValid || eval.Reason != "final" {
		t.Errorf("got verdict=%q reason=%q, want verdict=%q reason=%q", eval.Verdict, eval.Reason, VerdictValid, "final")
	}
}

func TestParseEvaluationNoJSON(t *testing.T) {
	if _, err := parseEvaluation("no json here"); err == nil {
		t.Fatal("parseEvaluation() error = nil, want error")
	}
}

func TestParseEvaluationInvalidVerdict(t *testing.T) {
	output := "{\"verdict\": \"maybe\", \"reason\": \"?\"}"
	if _, err := parseEvaluation(output); err == nil {
		t.Fatal("parseEvaluation() error = nil, want error")
	}
}

func TestBuildPrompt(t *testing.T) {
	comment := &Comment{URL: "https://example.com/1", Path: "main.go", Line: 10, DiffHunk: "@@ -1 +1 @@", Body: "untrusted body"}

	without := buildPrompt("judge this", "", false, "owner/repo", 1, comment)
	if strings.Contains(without, rubberDuckRequest) {
		t.Errorf("buildPrompt() with rubberDuck=false contains rubber duck request")
	}

	with := buildPrompt("judge this", "", true, "owner/repo", 1, comment)
	bodyIdx := strings.Index(with, comment.Body)
	duckIdx := strings.Index(with, rubberDuckRequest)
	contractIdx := strings.Index(with, "output your final judgement")
	if bodyIdx < 0 || duckIdx < 0 || contractIdx < 0 {
		t.Fatalf("buildPrompt() missing expected sections: %q", with)
	}
	if !(bodyIdx < duckIdx && duckIdx < contractIdx) {
		t.Errorf("buildPrompt() sections out of order: body=%d duck=%d contract=%d", bodyIdx, duckIdx, contractIdx)
	}
}

func TestBuildBatchPrompt(t *testing.T) {
	comments := []*Comment{
		{CommentID: 1, URL: "https://example.com/1", Path: "main.go", Line: 10, DiffHunk: "@@ -1 +1 @@", Body: "first"},
		{CommentID: 2, URL: "https://example.com/2", Path: "main.go", Line: 20, DiffHunk: "@@ -1 +1 @@", Body: "second"},
		{CommentID: 3, URL: "https://example.com/3", Path: "other.go", Line: 5, DiffHunk: "@@ -2 +2 @@", Body: "third"},
	}
	got := buildBatchPrompt("judge these", "", false, "owner/repo", 1, comments)

	for _, want := range []string{"comment_id: 1", "comment_id: 2", "comment_id: 3", "first", "second", "third", "(same as comment 1)"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildBatchPrompt() missing %q in output: %q", want, got)
		}
	}
	if strings.Count(got, "@@ -1 +1 @@") != 1 {
		t.Errorf("buildBatchPrompt() repeated an identical diff hunk instead of deduplicating it")
	}
}

func TestParseBatchEvaluation(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		output := "```json\n[{\"comment_id\": 1, \"verdict\": \"valid\", \"reason\": \"ok\"}, {\"comment_id\": 2, \"verdict\": \"invalid\", \"reason\": \"no\"}]\n```\n"
		evals, err := parseBatchEvaluation(output)
		if err != nil {
			t.Fatalf("parseBatchEvaluation() error = %v", err)
		}
		if len(evals) != 2 || evals[1].Verdict != VerdictValid || evals[2].Verdict != VerdictInvalid {
			t.Errorf("parseBatchEvaluation() = %+v, want 2 evaluations for comments 1 and 2", evals)
		}
	})

	t.Run("partial missing", func(t *testing.T) {
		output := "```json\n[{\"comment_id\": 1, \"verdict\": \"valid\", \"reason\": \"ok\"}]\n```\n"
		evals, err := parseBatchEvaluation(output)
		if err != nil {
			t.Fatalf("parseBatchEvaluation() error = %v", err)
		}
		if len(evals) != 1 {
			t.Errorf("parseBatchEvaluation() = %+v, want 1 evaluation", evals)
		}
	})

	t.Run("invalid verdict", func(t *testing.T) {
		output := "```json\n[{\"comment_id\": 1, \"verdict\": \"maybe\", \"reason\": \"?\"}]\n```\n"
		if _, err := parseBatchEvaluation(output); err == nil {
			t.Fatal("parseBatchEvaluation() error = nil, want error")
		}
	})

	t.Run("missing comment id", func(t *testing.T) {
		output := "```json\n[{\"verdict\": \"valid\", \"reason\": \"ok\"}]\n```\n"
		if _, err := parseBatchEvaluation(output); err == nil {
			t.Fatal("parseBatchEvaluation() error = nil, want error")
		}
	})

	t.Run("no fence fallback", func(t *testing.T) {
		output := "[{\"comment_id\": 5, \"verdict\": \"unclear\", \"reason\": \"?\"}]"
		evals, err := parseBatchEvaluation(output)
		if err != nil {
			t.Fatalf("parseBatchEvaluation() error = %v", err)
		}
		if len(evals) != 1 || evals[5].Verdict != VerdictUnclear {
			t.Errorf("parseBatchEvaluation() = %+v, want 1 evaluation for comment 5", evals)
		}
	})

	t.Run("no json", func(t *testing.T) {
		if _, err := parseBatchEvaluation("no json here"); err == nil {
			t.Fatal("parseBatchEvaluation() error = nil, want error")
		}
	})
}

func TestEvaluateParsesResultDespiteNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakeCopilotBin requires a POSIX shell")
	}
	output := "```json\n{\"verdict\": \"valid\", \"reason\": \"ok\"}\n```\nAI Credits 5 (1s)"
	opts := EvaluateOptions{Bin: fakeCopilotBin(t, output, 137), Prompt: "judge"}
	comment := &Comment{CommentID: 1, URL: "https://example.com/1"}

	eval, usage, err := Evaluate(context.Background(), opts, "owner/repo", 1, comment)
	if err != nil {
		t.Fatalf("Evaluate() error = %v, want nil (a printed verdict should win over a killed process)", err)
	}
	if eval == nil || eval.Verdict != VerdictValid {
		t.Errorf("Evaluate() eval = %+v, want verdict %q", eval, VerdictValid)
	}
	if usage == nil || usage.AICredits != 5 {
		t.Errorf("Evaluate() usage = %+v, want AICredits 5", usage)
	}
}

func TestEvaluateBatchParsesResultDespiteNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakeCopilotBin requires a POSIX shell")
	}
	output := "```json\n[{\"comment_id\": 1, \"verdict\": \"valid\", \"reason\": \"ok\"}]\n```\nAI Credits 5 (1s)"
	opts := EvaluateOptions{Bin: fakeCopilotBin(t, output, 137), Prompt: "judge"}
	comments := []*Comment{{CommentID: 1, URL: "https://example.com/1"}}

	evals, usage, err := EvaluateBatch(context.Background(), opts, "owner/repo", 1, comments)
	if err != nil {
		t.Fatalf("EvaluateBatch() error = %v, want nil (printed verdicts should win over a killed process)", err)
	}
	if len(evals) != 1 || evals[1].Verdict != VerdictValid {
		t.Errorf("EvaluateBatch() evals = %+v, want 1 evaluation with verdict %q", evals, VerdictValid)
	}
	if usage == nil || usage.AICredits != 5 {
		t.Errorf("EvaluateBatch() usage = %+v, want AICredits 5", usage)
	}
}

func TestEvaluateReportsRunErrorWhenOutputIsUnparseable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakeCopilotBin requires a POSIX shell")
	}
	opts := EvaluateOptions{Bin: fakeCopilotBin(t, "no verdict was printed before being killed", 137), Prompt: "judge"}
	comment := &Comment{CommentID: 1, URL: "https://example.com/1"}

	if _, _, err := Evaluate(context.Background(), opts, "owner/repo", 1, comment); err == nil {
		t.Fatal("Evaluate() error = nil, want error when no verdict can be parsed")
	}
}

func TestParseUsage(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   float64
	}{
		{"plain", "Changes    +0 -0\nAI Credits 7.5 (4s)\nTokens     ↑ 25.6k\n", 7.5},
		{"integer", "AI Credits 12 (1s)\n", 12},
		{"thousand separator", "AI Credits 1,234.5 (9s)\n", 1234.5},
		{"k suffix", "AI Credits 1.2k (9s)\n", 1200},
		{"last footer wins", "AI Credits 1 (1s)\nAI Credits 3.25 (2s)\n", 3.25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := parseUsage(tt.output)
			if usage == nil {
				t.Fatal("parseUsage() = nil, want usage")
			}
			if usage.AICredits != tt.want {
				t.Errorf("AICredits = %v, want %v", usage.AICredits, tt.want)
			}
		})
	}
}

func TestParseUsageNoFooter(t *testing.T) {
	if usage := parseUsage("no usage footer here"); usage != nil {
		t.Errorf("parseUsage() = %+v, want nil", usage)
	}
}
