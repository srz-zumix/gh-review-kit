package copilot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// claudeResultJSON renders a Claude Code result message whose text is result.
func claudeResultJSON(t *testing.T, result string, cost float64, denials []claudeDenial) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":               "result",
		"subtype":            "success",
		"is_error":           false,
		"result":             result,
		"total_cost_usd":     cost,
		"permission_denials": denials,
	})
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}
	return string(b)
}

func TestBuildClaudeArgs(t *testing.T) {
	tests := []struct {
		name string
		opts EvaluateOptions
		want []string
	}{
		{
			name: "minimal",
			opts: EvaluateOptions{},
			want: []string{"-p", "--output-format", "json"},
		},
		{
			name: "new session",
			opts: EvaluateOptions{Agent: "reviewer", SessionID: "sess-1", Model: "sonnet"},
			want: []string{"-p", "--output-format", "json", "--agent", "reviewer", "--session-id", "sess-1", "--model", "sonnet"},
		},
		{
			name: "resumed session",
			opts: EvaluateOptions{SessionID: "sess-1", ResumeSession: true},
			want: []string{"-p", "--output-format", "json", "--resume", "sess-1"},
		},
		{
			name: "allow all tools is auto mode",
			opts: EvaluateOptions{AutoApprove: true},
			want: []string{"-p", "--output-format", "json", "--permission-mode", "auto"},
		},
		{
			name: "sandbox through settings",
			opts: EvaluateOptions{Sandbox: true},
			want: []string{"-p", "--output-format", "json", "--settings", claudeSandboxSettings},
		},
		{
			name: "rubber duck does not change args",
			opts: EvaluateOptions{RubberDuck: true},
			want: []string{"-p", "--output-format", "json"},
		},
		{
			name: "extra args appended last",
			opts: EvaluateOptions{AutoApprove: true, ExtraArgs: []string{"--allowedTools", "Bash(git *)"}},
			want: []string{"-p", "--output-format", "json", "--permission-mode", "auto", "--allowedTools", "Bash(git *)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildClaudeArgs(tt.opts); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildClaudeArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseClaudeResult(t *testing.T) {
	result := `{"type":"result","is_error":false,"result":"done","total_cost_usd":0.25}`
	tests := []struct {
		name   string
		stdout string
		want   string
		ok     bool
	}{
		{"single object", result, "done", true},
		{"array of messages", `[{"type":"system"},` + result + `]`, "done", true},
		{"one message per line", `{"type":"system"}` + "\n" + result + "\n", "done", true},
		{"no result message", `{"type":"system"}`, "", false},
		{"not json", "Error: something broke", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseClaudeResult(tt.stdout)
			if ok != tt.ok {
				t.Fatalf("parseClaudeResult() ok = %v, want %v", ok, tt.ok)
			}
			if ok && got.Result != tt.want {
				t.Errorf("parseClaudeResult() result = %q, want %q", got.Result, tt.want)
			}
		})
	}
}

func TestClaudeDeniedCalls(t *testing.T) {
	calls := claudeDeniedCalls([]claudeDenial{
		{ToolName: "Bash", ToolInput: map[string]any{"command": "go test ./...\n  -v"}},
		{ToolName: "Read", ToolInput: map[string]any{"file_path": "/etc/hosts"}},
		{ToolName: "WebFetch", ToolInput: map[string]any{"url": "https://example.com"}},
	})
	if len(calls) != 3 {
		t.Fatalf("claudeDeniedCalls() returned %d calls, want 3", len(calls))
	}
	if calls[0].Tool != "shell" || calls[0].Label != "Bash(go test ./... -v)" || len(calls[0].Body) != 1 {
		t.Errorf("Bash call = %+v, want a shell call labelled with its collapsed command", calls[0])
	}
	if calls[1].Tool != "Read" || !reflect.DeepEqual(calls[1].Paths, []string{"/etc/hosts"}) {
		t.Errorf("Read call = %+v, want its file path", calls[1])
	}
	if calls[2].Label != "WebFetch" || len(calls[2].Paths) != 0 {
		t.Errorf("WebFetch call = %+v, want a bare label", calls[2])
	}
}

func TestExistingClaudeOptions(t *testing.T) {
	tests := []struct {
		name       string
		opts       EvaluateOptions
		wantAuto   bool
		wantBypass bool
		wantRules  []string
		wantDirs   []string
	}{
		{"nothing", EvaluateOptions{}, false, false, nil, nil},
		{"allow all tools", EvaluateOptions{AutoApprove: true}, true, false, nil, nil},
		{"permission mode auto", EvaluateOptions{ExtraArgs: []string{"--permission-mode", "auto"}}, true, false, nil, nil},
		{"permission mode= bypass", EvaluateOptions{ExtraArgs: []string{"--permission-mode=bypassPermissions"}}, false, true, nil, nil},
		{"skip permissions", EvaluateOptions{ExtraArgs: []string{"--dangerously-skip-permissions"}}, false, true, nil, nil},
		{"dontAsk is neither", EvaluateOptions{ExtraArgs: []string{"--permission-mode", "dontAsk"}}, false, false, nil, nil},
		{
			"variadic allowed tools",
			EvaluateOptions{ExtraArgs: []string{"--allowedTools", "Bash(git log *)", "Read", "--model", "x"}},
			false, false, []string{"Bash(git log *)", "Read"}, nil,
		},
		{
			"comma separated allowed tools",
			EvaluateOptions{ExtraArgs: []string{"--allowed-tools=Bash(git log *),Edit"}},
			false, false, []string{"Bash(git log *)", "Edit"}, nil,
		},
		{
			"add dir",
			EvaluateOptions{ExtraArgs: []string{"--add-dir", "/tmp/a", "/tmp/b/"}},
			false, false, nil, []string{"/tmp/a", "/tmp/b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := existingClaudeOptions(tt.opts)
			if got.auto != tt.wantAuto || got.bypass != tt.wantBypass {
				t.Errorf("auto/bypass = %v/%v, want %v/%v", got.auto, got.bypass, tt.wantAuto, tt.wantBypass)
			}
			rules := make([]string, 0, len(got.rules))
			for r := range got.rules {
				rules = append(rules, r)
			}
			if len(rules) != len(tt.wantRules) {
				t.Errorf("rules = %v, want %v", rules, tt.wantRules)
			}
			for _, r := range tt.wantRules {
				if !got.rules[r] {
					t.Errorf("rules = %v, want %q included", rules, r)
				}
			}
			if !reflect.DeepEqual(got.dirs, tt.wantDirs) {
				t.Errorf("dirs = %v, want %v", got.dirs, tt.wantDirs)
			}
		})
	}
}

func TestRecommendClaudePermissions(t *testing.T) {
	outside := t.TempDir()
	other := t.TempDir()
	bash := func(command string) deniedCall {
		return deniedCall{Label: "Bash(" + command + ")", Tool: "shell", Body: []string{command}}
	}

	tests := []struct {
		name         string
		opts         EvaluateOptions
		calls        []deniedCall
		want         []string
		wantWritable []string
	}{
		{"no calls", EvaluateOptions{}, nil, nil, nil},
		{
			"bash commands become allow rules",
			EvaluateOptions{},
			[]deniedCall{bash("go test ./..."), bash("git status && ls")},
			[]string{"--allowedTools=Bash(git *)", "--allowedTools=Bash(go *)", "--allowedTools=Bash(ls *)"},
			nil,
		},
		{
			"already allowed rule is omitted",
			EvaluateOptions{ExtraArgs: []string{"--allowedTools", "Bash(go *)"}},
			[]deniedCall{bash("go test ./...")},
			nil,
			nil,
		},
		{
			"bare Bash rule covers every command",
			EvaluateOptions{ExtraArgs: []string{"--allowedTools=Bash"}},
			[]deniedCall{bash("go test ./...")},
			nil,
			nil,
		},
		{
			"bypass leaves nothing to recommend",
			EvaluateOptions{ExtraArgs: []string{"--permission-mode", "bypassPermissions"}},
			[]deniedCall{bash("go test ./...")},
			nil,
			nil,
		},
		{
			"unknown command falls back to auto mode",
			EvaluateOptions{},
			[]deniedCall{{Label: "Bash", Tool: "shell"}},
			[]string{"--permission-mode=auto"},
			nil,
		},
		{
			"unknown command under auto mode recommends nothing",
			EvaluateOptions{AutoApprove: true},
			[]deniedCall{{Label: "Bash", Tool: "shell"}},
			nil,
			nil,
		},
		{
			"shell arguments outside the working directory",
			EvaluateOptions{},
			[]deniedCall{bash("ls " + outside), bash("touch " + filepath.Join(other, "f"))},
			[]string{"--allowedTools=Bash(ls *)", "--allowedTools=Bash(touch *)", "--add-dir=" + outside, "--add-dir=" + other},
			[]string{other},
		},
		{
			"read outside the working directory",
			EvaluateOptions{},
			[]deniedCall{{Label: "Read", Tool: "Read", Paths: []string{filepath.Join(outside, "f.txt")}}},
			[]string{"--add-dir=" + outside},
			nil,
		},
		{
			"directory already added",
			EvaluateOptions{ExtraArgs: []string{"--add-dir", outside}},
			[]deniedCall{{Label: "Read", Tool: "Read", Paths: []string{filepath.Join(outside, "f.txt")}}},
			nil,
			nil,
		},
		{
			"write tools share one Edit rule and need write access",
			EvaluateOptions{},
			[]deniedCall{
				{Label: "Write", Tool: "Write", Paths: []string{filepath.Join(outside, "a")}},
				{Label: "Edit", Tool: "Edit", Paths: []string{filepath.Join(outside, "b")}},
			},
			[]string{"--allowedTools=Edit", "--add-dir=" + outside},
			[]string{outside},
		},
		{
			"tool without a path is allowed by name",
			EvaluateOptions{},
			[]deniedCall{{Label: "WebFetch", Tool: "WebFetch"}},
			[]string{"--allowedTools=WebFetch"},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, writable := recommendClaudePermissions(tt.opts, tt.calls)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("recommendClaudePermissions() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(writable, tt.wantWritable) {
				t.Errorf("recommendClaudePermissions() writable = %v, want %v", writable, tt.wantWritable)
			}
		})
	}
}

func TestNeedsToolPermissionWarningClaude(t *testing.T) {
	tests := []struct {
		name          string
		allowAllTools bool
		extraArgs     []string
		want          bool
	}{
		{"nothing authorized", false, nil, true},
		{"allow all tools is auto mode", true, nil, false},
		{"allowedTools", false, []string{"--allowedTools", "Bash(git *)"}, false},
		{"permission mode auto", false, []string{"--permission-mode", "auto"}, false},
		{"skip permissions", false, []string{"--dangerously-skip-permissions"}, false},
		{"copilot option does not count", false, []string{"--allow-all-tools"}, true},
		{"dontAsk denies everything", false, []string{"--permission-mode", "dontAsk"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsToolPermissionWarning(EvaluatorClaude, tt.allowAllTools, tt.extraArgs); got != tt.want {
				t.Errorf("NeedsToolPermissionWarning() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateWithClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakeCopilotBin requires a POSIX shell")
	}
	out := claudeResultJSON(t, "```json\n{\"verdict\": \"invalid\", \"reason\": \"false positive\"}\n```", 0.5,
		[]claudeDenial{{ToolName: "Bash", ToolInput: map[string]any{"command": "go test ./..."}}})
	opts := EvaluateOptions{Evaluator: EvaluatorClaude, Bin: fakeCopilotBin(t, out, 0), Prompt: "judge"}
	comment := &Comment{CommentID: 1, URL: "https://example.com/1"}

	eval, usage, err := Evaluate(context.Background(), opts, "owner/repo", 1, comment)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if eval == nil || eval.Verdict != VerdictInvalid {
		t.Errorf("Evaluate() eval = %+v, want verdict %q", eval, VerdictInvalid)
	}
	if usage == nil || usage.CostUSD != 0.5 {
		t.Fatalf("Evaluate() usage = %+v, want CostUSD 0.5", usage)
	}
	if !reflect.DeepEqual(usage.Denials, []string{"Bash(go test ./...)"}) {
		t.Errorf("Evaluate() denials = %v, want the denied Bash call", usage.Denials)
	}
	if !reflect.DeepEqual(usage.Recommendations, []string{"--allowedTools=Bash(go *)"}) {
		t.Errorf("Evaluate() recommendations = %v, want an allow rule for go", usage.Recommendations)
	}
}

func TestEvaluateBatchWithClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakeCopilotBin requires a POSIX shell")
	}
	out := claudeResultJSON(t, "```json\n[{\"comment_id\": 1, \"verdict\": \"valid\", \"reason\": \"ok\"}]\n```", 1.5, nil)
	opts := EvaluateOptions{Evaluator: EvaluatorClaude, Bin: fakeCopilotBin(t, out, 0), Prompt: "judge"}
	comments := []*Comment{{CommentID: 1, URL: "https://example.com/1"}}

	evals, usage, err := EvaluateBatch(context.Background(), opts, "owner/repo", 1, comments)
	if err != nil {
		t.Fatalf("EvaluateBatch() error = %v", err)
	}
	if len(evals) != 1 || evals[1].Verdict != VerdictValid {
		t.Errorf("EvaluateBatch() evals = %+v, want 1 evaluation with verdict %q", evals, VerdictValid)
	}
	if usage == nil || usage.CostUSD != 1.5 {
		t.Errorf("EvaluateBatch() usage = %+v, want CostUSD 1.5", usage)
	}
}

func TestEvaluateWithClaudeReportsLimitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fakeCopilotBin requires a POSIX shell")
	}
	out := `{"type":"result","is_error":true,"result":"Claude AI usage limit reached","total_cost_usd":0}`
	opts := EvaluateOptions{Evaluator: EvaluatorClaude, Bin: fakeCopilotBin(t, out, 1), Prompt: "judge"}
	comment := &Comment{CommentID: 1, URL: "https://example.com/1"}

	_, usage, err := Evaluate(context.Background(), opts, "owner/repo", 1, comment)
	if err == nil {
		t.Fatal("Evaluate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "failed to run Claude Code") || !strings.Contains(err.Error(), "usage limit reached") {
		t.Errorf("Evaluate() error = %q, want it to report what failed", err)
	}
	if usage == nil || !usage.QuotaExceeded {
		t.Errorf("Evaluate() usage = %+v, want QuotaExceeded", usage)
	}
}

func TestEvaluateWithClaudePipesPromptToStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake script requires a POSIX shell")
	}
	dir := t.TempDir()
	stdinFile := filepath.Join(dir, "stdin")
	out := claudeResultJSON(t, "```json\n{\"verdict\": \"valid\", \"reason\": \"ok\"}\n```", 0, nil)
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > '" + stdinFile + "'\ncat <<'EOF'\n" + out + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude script: %v", err)
	}
	opts := EvaluateOptions{Evaluator: EvaluatorClaude, Bin: path, Prompt: "-judge carefully"}
	comment := &Comment{CommentID: 1, URL: "https://example.com/1"}

	if _, _, err := Evaluate(context.Background(), opts, "owner/repo", 1, comment); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	got, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("failed to read captured stdin: %v", err)
	}
	if !strings.HasPrefix(string(got), "-judge carefully") {
		t.Errorf("stdin = %q, want it to start with the prompt", got)
	}
}

func TestEvaluateAllSequentialResumesClaudeSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake script requires a POSIX shell")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	out := claudeResultJSON(t, "```json\n{\"verdict\": \"valid\", \"reason\": \"ok\"}\n```", 0, nil)
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > /dev/null\necho \"$*\" >> '" + argsFile + "'\ncat <<'EOF'\n" + out + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake claude script: %v", err)
	}
	opts := EvaluateOptions{Evaluator: EvaluatorClaude, Bin: path, Prompt: "judge", SessionID: "sess-1"}
	comments := []*Comment{{CommentID: 1}, {CommentID: 2}}

	results, _ := evaluateAllSequential(context.Background(), opts, "owner/repo", 1, comments)
	for _, r := range results {
		if r.Error != "" {
			t.Fatalf("evaluateAllSequential() error = %q", r.Error)
		}
	}
	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read captured args: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "--session-id sess-1") || !strings.Contains(lines[1], "--resume sess-1") {
		t.Errorf("invocations = %q, want the first to create and the second to resume the session", lines)
	}
}
