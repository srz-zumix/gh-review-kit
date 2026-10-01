package copilot

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestBuildAliasExpansion(t *testing.T) {
	flags := []AliasFlag{
		{Name: "prompt", Values: []string{"it's fine"}},
		{Name: "language", Values: []string{"JP"}},
		{Name: "author", Values: []string{"a", "b"}},
	}
	got := BuildAliasExpansion(EvaluatorClaude, flags, []string{"--allowedTools=Bash(go *)"})
	want := `evaluator=claude; case "$1" in copilot|claude) evaluator="$1"; shift;; esac; ` +
		`gh review-kit copilot comments --evaluate="$evaluator" '--prompt=it'\''s fine' --language=JP --author=a --author=b "$@" -- '--allowedTools=Bash(go *)'`
	if got != want {
		t.Errorf("BuildAliasExpansion() =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildAliasExpansion_Shell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	flags := []AliasFlag{{Name: "prompt", Values: []string{"it's $HOME `x`"}}}
	expansion := BuildAliasExpansion(EvaluatorCopilot, flags, nil)
	// Replace the gh invocation with printf to observe the words the shell produces.
	expansion = "gh() { printf '%s\\n' \"$@\"; }; " + expansion
	run := func(args ...string) string {
		out, err := exec.Command(sh, append([]string{"-c", expansion, "--"}, args...)...).Output()
		if err != nil {
			t.Fatalf("sh failed: %v", err)
		}
		return string(out)
	}
	wantDefault := "review-kit\ncopilot\ncomments\n--evaluate=copilot\n--prompt=it's $HOME `x`\n"
	if got := run(); got != wantDefault {
		t.Errorf("default run = %q, want %q", got, wantDefault)
	}
	wantClaude := "review-kit\ncopilot\ncomments\n--evaluate=claude\n--prompt=it's $HOME `x`\n--dryrun\n"
	if got := run("claude", "--dryrun"); got != wantClaude {
		t.Errorf("claude run = %q, want %q", got, wantClaude)
	}
}

func TestCollectAliasFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("prompt", "", "")
	fs.String("prompt-file", "", "")
	fs.String("alias-set", "", "")
	fs.StringSlice("author", nil, "")
	fs.Bool("sandbox", false, "")
	fs.Bool("auto-approve", false, "")
	if err := fs.Parse([]string{"--alias-set", "x", "--prompt-file", "p.md", "--author", "a,b", "--sandbox"}); err != nil {
		t.Fatal(err)
	}

	got, err := CollectAliasFlags(fs, []string{"alias-set"}, []string{"prompt-file"})
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("p.md")
	want := []AliasFlag{
		{Name: "author", Values: []string{"a", "b"}},
		{Name: "prompt-file", Values: []string{abs}},
		{Name: "sandbox", Values: []string{"true"}},
	}
	if len(got) != len(want) {
		t.Fatalf("CollectAliasFlags() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Name != want[i].Name || len(got[i].Values) != len(want[i].Values) {
			t.Fatalf("CollectAliasFlags()[%d] = %v, want %v", i, got[i], want[i])
		}
		for j := range want[i].Values {
			if got[i].Values[j] != want[i].Values[j] {
				t.Errorf("CollectAliasFlags()[%d].Values[%d] = %q, want %q", i, j, got[i].Values[j], want[i].Values[j])
			}
		}
	}
}
