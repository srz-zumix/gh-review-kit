package copilot

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func denied(tool string, body ...string) deniedCall {
	return deniedCall{Label: "Do something (" + tool + ")", Tool: tool, Body: body}
}

func TestRecommendToolOptions(t *testing.T) {
	tests := []struct {
		name  string
		opts  EvaluateOptions
		calls []deniedCall
		want  []string
	}{
		{
			name: "no denials",
		},
		{
			name:  "every command of a shell pipeline",
			calls: []deniedCall{denied("shell", "cd /nowhere && grep -rn foo | head -3")},
			want:  []string{"--allow-tool=shell(cd:*)", "--allow-tool=shell(grep:*)", "--allow-tool=shell(head:*)"},
		},
		{
			name:  "leading environment assignments are skipped",
			calls: []deniedCall{denied("sandboxed shell", "VAR=1 go test ./...")},
			want:  []string{"--allow-tool=shell(go:*)"},
		},
		{
			name:  "absolute command path uses its base name",
			calls: []deniedCall{denied("shell", "/usr/local/bin/rg pattern")},
			want:  []string{"--allow-tool=shell(rg:*)"},
		},
		{
			name:  "display name is the command itself",
			calls: []deniedCall{denied("rm", "rm recommended/zz_probe_test.go")},
			want:  []string{"--allow-tool=shell(rm:*)"},
		},
		{
			name:  "body that is not a command line falls back",
			calls: []deniedCall{denied("grep", "\"func List\" in *.go (~)")},
			want:  []string{"--allow-all-tools"},
		},
		{
			name:  "display name unrelated to the body falls back",
			calls: []deniedCall{denied("view", "cat hosts")},
			want:  []string{"--allow-all-tools"},
		},
		{
			name:  "option leading a mis-split segment is not a command",
			calls: []deniedCall{denied("shell", "--allow-all baz")},
			want:  []string{"--allow-all-tools"},
		},
		{
			name:  "allow all tools already in effect",
			opts:  EvaluateOptions{AllowAllTools: true},
			calls: []deniedCall{denied("shell", "ls somewhere")},
		},
		{
			name:  "allow tool already forwarded",
			opts:  EvaluateOptions{ExtraArgs: []string{"--allow-tool", "shell(ls:*)"}},
			calls: []deniedCall{denied("shell", "ls somewhere")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recommendPermissions(tt.opts, tt.calls)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("recommendPermissions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRecommendDirOptions(t *testing.T) {
	root := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("failed to create nested directory: %v", err)
	}
	file := filepath.Join(nested, "go.work")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	tests := []struct {
		name  string
		opts  EvaluateOptions
		calls []deniedCall
		want  []string
	}{
		{
			name:  "existing directory",
			calls: []deniedCall{denied("shell", "ls "+nested)},
			want:  []string{"--add-dir=" + nested},
		},
		{
			name:  "file grants its directory",
			calls: []deniedCall{denied("shell", "head -20 "+file)},
			want:  []string{"--add-dir=" + nested},
		},
		{
			name:  "missing path falls back to its nearest existing ancestor",
			calls: []deniedCall{denied("shell", "ls "+filepath.Join(nested, "gone", "missing.go"))},
			want:  []string{"--add-dir=" + nested},
		},
		{
			name:  "root and system directories are never granted",
			calls: []deniedCall{denied("shell", "find / -name x && ls /usr/bin && ls /dev")},
		},
		{
			name:  "globbed paths are not granted",
			calls: []deniedCall{denied("shell", "ls "+filepath.Join(root, "*", "b"))},
		},
		{
			name: "nested directories collapse into their ancestor",
			calls: []deniedCall{
				denied("shell", "ls "+nested),
				denied("shell", "ls "+root),
			},
			want: []string{"--add-dir=" + root},
		},
		{
			name:  "add dir already forwarded",
			opts:  EvaluateOptions{ExtraArgs: []string{"--add-dir=" + nested}},
			calls: []deniedCall{denied("shell", "ls "+nested)},
		},
		{
			name:  "path bypass already in effect",
			opts:  EvaluateOptions{ExtraArgs: []string{"--allow-all-paths"}},
			calls: []deniedCall{denied("shell", "ls "+nested)},
		},
		{
			name:  "sandbox grants the working directory",
			opts:  EvaluateOptions{Sandbox: true},
			calls: []deniedCall{denied("shell", "ls "+filepath.Join(wd, "pkg"))},
		},
		{
			name:  "path wrapped across body lines is rejoined",
			calls: []deniedCall{denied("shell", "sed -n '1,2p' "+file[:len(root)+3], file[len(root)+3:])},
			want:  []string{"--add-dir=" + nested},
		},
		{
			name:  "body lines that are not a wrapped path stay separate",
			calls: []deniedCall{denied("shell", "ls "+nested, "&& ls "+root)},
			want:  []string{"--add-dir=" + root},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.AllowAllTools = true
			got := recommendPermissions(tt.opts, tt.calls)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("recommendPermissions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewUsageRecommendsOptionsForDeniedCalls(t *testing.T) {
	dir := t.TempDir()
	output := "✗ Inspect go.work (sandboxed shell)\n" +
		"  │ cat " + filepath.Join(dir, "go.work") + " && ls " + dir + "\n" +
		"  └ Permission denied and could not request permission from user\n" +
		"\nAI Credits 5 (1s)\n"

	usage := newUsage(EvaluateOptions{AllowAllTools: true, Sandbox: true}, output)
	if usage == nil {
		t.Fatal("newUsage() = nil, want a usage")
	}
	if want := []string{"Inspect go.work (sandboxed shell)"}; !reflect.DeepEqual(usage.Denials, want) {
		t.Errorf("Denials = %v, want %v", usage.Denials, want)
	}
	if want := []string{"--add-dir=" + dir}; !reflect.DeepEqual(usage.Recommendations, want) {
		t.Errorf("Recommendations = %v, want %v", usage.Recommendations, want)
	}
}

func TestNewUsageReportsQuotaExceededWithoutAFooter(t *testing.T) {
	usage := newUsage(EvaluateOptions{}, "You have exceeded your monthly quota (Request ID: abc)\n")
	if usage == nil {
		t.Fatal("newUsage() = nil, want a usage reporting the quota")
	}
	if !usage.QuotaExceeded {
		t.Error("QuotaExceeded = false, want true")
	}
}

func TestSandboxSettingsHint(t *testing.T) {
	got := SandboxSettingsHint([]string{"--allow-tool=shell(grep:*)", "--add-dir=/work/repo", "--add-dir=/tmp"})
	want := `{"sandbox":{"userPolicy":{"filesystem":{"readonlyPaths":["/work/repo","/tmp"]}}}}`
	if got != want {
		t.Errorf("SandboxSettingsHint() = %q, want %q", got, want)
	}
	if got := SandboxSettingsHint([]string{"--allow-all-tools"}); got != "" {
		t.Errorf("SandboxSettingsHint() = %q, want \"\"", got)
	}
}

func TestFormatRecommendations(t *testing.T) {
	got := FormatRecommendations([]string{"--allow-tool=shell(grep:*)", "--add-dir=/work/repo", "--allow-all-tools"})
	want := "--allow-tool='shell(grep:*)' --add-dir=/work/repo --allow-all-tools"
	if got != want {
		t.Errorf("FormatRecommendations() = %q, want %q", got, want)
	}
}
