package copilot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEvaluateAllBatchRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	const incomplete = `[{"comment_id":1,"verdict":"valid","reason":"keep"},{"comment_id":2,"verdict":"valid","reason":"duplicate"},{"comment_id":2,"verdict":"invalid","reason":"duplicate"}]`
	const complete = `[{"comment_id":1,"verdict":"valid","reason":"keep"},{"comment_id":2,"verdict":"valid","reason":"ok"},{"comment_id":3,"verdict":"valid","reason":"ok"}]`
	const recovered = `[{"comment_id":1,"verdict":"invalid","reason":"must not overwrite"},{"comment_id":2,"verdict":"invalid","reason":"recovered"},{"comment_id":3,"verdict":"valid","reason":"recovered"}]`
	const unrelated = `[{"comment_id":99,"verdict":"valid","reason":"unrelated"}]`
	for _, evaluator := range []Evaluator{EvaluatorCopilot, EvaluatorClaude} {
		for _, tt := range []struct {
			name       string
			first      string
			second     string
			quota      bool
			wantRetry  bool
			wantErrors int
			wantReason string
		}{
			{name: "recover missing and duplicate", first: incomplete, second: recovered, wantRetry: true},
			{name: "retry bounded", first: incomplete, second: incomplete, wantRetry: true, wantErrors: 2},
			{name: "retry parse failure", first: incomplete, second: "no JSON", wantRetry: true, wantErrors: 2},
			{name: "complete needs no retry", first: complete},
			{name: "quota prevents retry", first: incomplete, quota: true, wantErrors: 2},
			{name: "recover all missing", first: unrelated, second: recovered, wantRetry: true, wantReason: "must not overwrite"},
			{name: "no verdicts twice", first: unrelated, second: unrelated, wantRetry: true, wantErrors: 3},
		} {
			t.Run(evaluator.DisplayName()+"/"+tt.name, func(t *testing.T) {
				dir := t.TempDir()
				for name, output := range map[string]string{"first": tt.first, "second": tt.second} {
					if evaluator.IsClaude() {
						cost := 1.0
						if name == "second" {
							cost = 2
						}
						if tt.quota {
							output += "\nYou have exceeded your quota"
						}
						encoded, err := json.Marshal(claudeResult{Type: "result", IsError: tt.quota, Result: output, TotalCostUSD: cost, PermissionDenials: []claudeDenial{{ToolName: "Bash", ToolInput: map[string]any{"command": name}}}})
						if err != nil {
							t.Fatal(err)
						}
						output = string(encoded)
					} else {
						output += "\nAI Credits 5 (1s)"
						if tt.quota {
							output += "\nYou have exceeded your monthly quota"
						}
					}
					if err := os.WriteFile(filepath.Join(dir, name), []byte(output+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(dir, "evaluator")
				script := "#!/bin/sh\ncd \"$(dirname \"$0\")\"\nif test -f first-args; then\n  test ! -f second-args || exit 99\n  name=second\nelse\n  name=first\nfi\nprintf '%s\\n' \"$@\" > \"$name-args\"\n"
				if evaluator.IsClaude() {
					script += "cat > \"$name-prompt\"\n"
				}
				script += "cat \"$name\"\n"
				if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				opts := EvaluateOptions{Evaluator: evaluator, Bin: path, Prompt: "judge and fix", SessionID: "session-1", Timeout: time.Second}
				comments := []*Comment{{CommentID: 1}, {CommentID: 2}, {CommentID: 3}}
				results, usage := evaluateAllBatch(context.Background(), opts, "owner/repo", 1, comments)
				wantReason := tt.wantReason
				if wantReason == "" {
					wantReason = "keep"
				}
				if len(results) != 3 || (tt.wantErrors < 3 && (results[0].Evaluation == nil || results[0].Evaluation.Reason != wantReason)) {
					t.Fatalf("normal evaluation lost: %+v", results)
				}
				errors := 0
				for _, res := range results {
					if res.Error != "" {
						errors++
					}
				}
				if errors != tt.wantErrors {
					t.Fatalf("errors = %d, want %d: %+v", errors, tt.wantErrors, results)
				}
				args, err := os.ReadFile(filepath.Join(dir, "second-args"))
				if !tt.wantRetry {
					if !os.IsNotExist(err) {
						t.Fatalf("unexpected recovery invocation: %s, %v", args, err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				sessionFlag := "--session-id"
				prompt := string(args)
				if evaluator.IsClaude() {
					sessionFlag = "--resume"
					data, err := os.ReadFile(filepath.Join(dir, "second-prompt"))
					if err != nil {
						t.Fatal(err)
					}
					prompt = string(data)
					if usage == nil || usage.CostUSD != 2 || len(usage.Denials) != 2 || len(results[0].Denials) != 2 {
						t.Fatalf("recovery usage not merged: %+v", usage)
					}
				}
				if !strings.Contains(string(args), sessionFlag+"\nsession-1\n") {
					t.Fatalf("recovery did not continue session: %s", args)
				}
				// Comment 1 was judged by the first batch unless that batch
				// covered nothing, so it is only re-sent in the all-missing case.
				if strings.Contains(prompt, "comment_id: 1") != (tt.first == unrelated) {
					t.Fatalf("unexpected recovery comment list: %s", prompt)
				}
				if !strings.Contains(prompt, "comment_id: 2") || !strings.Contains(prompt, "comment_id: 3") || !strings.Contains(prompt, "Do not edit files") || strings.Contains(prompt, "judge and fix") {
					t.Fatalf("unexpected recovery prompt: %s", prompt)
				}
			})
		}
	}
}

func TestBatchTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		comments int
		want     time.Duration
	}{
		{name: "scales with the comment count", timeout: 15 * time.Minute, comments: 4, want: time.Hour},
		{name: "a single comment keeps the timeout", timeout: 15 * time.Minute, comments: 1, want: 15 * time.Minute},
		{name: "no timeout stays unbounded", comments: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := batchTimeout(tt.timeout, tt.comments); got != tt.want {
				t.Errorf("batchTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAssignBatchResults(t *testing.T) {
	comments := []*Comment{
		{CommentID: 1},
		{CommentID: 2},
		{CommentID: 3},
		{CommentID: 4},
	}
	evals := map[int64]*Evaluation{
		1: {Verdict: VerdictValid, Reason: "ok"},
		3: {Verdict: VerdictInvalid, Reason: "no"},
		4: nil,
	}

	results := assignBatchResults(comments, evals)
	if len(results) != 4 {
		t.Fatalf("assignBatchResults() returned %d results, want 4", len(results))
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
	if results[3].Evaluation != nil || !strings.Contains(results[3].Error, "duplicate") {
		t.Errorf("results[3] = %+v, want duplicate evaluation error", results[3])
	}
}

// TestEvaluateAllBatchRecoveryKeepsUsage covers a recovery that stops on the
// quota limit before the CLI prints its usage footer: the counters the first
// invocation reported must survive.
func TestEvaluateAllBatchRecoveryKeepsUsage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake evaluator requires a POSIX shell")
	}
	dir := t.TempDir()
	first := `[{"comment_id":1,"verdict":"valid","reason":"keep"}]` + "\nAI Credits 5 (1s)\nTokens 1 (↑ 100 ↓ 20)"
	second := "You have exceeded your monthly quota"
	for name, output := range map[string]string{"first": first, "second": second} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(output+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "evaluator")
	script := "#!/bin/sh\ncd \"$(dirname \"$0\")\"\nif test -f first-args; then\n  name=second\nelse\n  name=first\nfi\nprintf '%s\\n' \"$@\" > \"$name-args\"\ncat \"$name\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := EvaluateOptions{Evaluator: EvaluatorCopilot, Bin: path, Prompt: "judge and fix", SessionID: "session-1", Timeout: time.Second}
	comments := []*Comment{{CommentID: 1}, {CommentID: 2}}
	_, usage := evaluateAllBatch(context.Background(), opts, "owner/repo", 1, comments)
	if usage == nil || !usage.QuotaExceeded {
		t.Fatalf("usage = %+v, want quota exceeded", usage)
	}
	if usage.AICredits != 5 || usage.InputTokens != 100 || usage.OutputTokens != 20 {
		t.Fatalf("usage counters lost: %+v", usage)
	}
}
