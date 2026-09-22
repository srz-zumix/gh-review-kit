package copilot

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectDenials(t *testing.T) {
	detectDenials := func(output string) []string {
		return denialLabels(detectDeniedCalls(output))
	}

	t.Run("single line command", func(t *testing.T) {
		output := "✗ Search (grep)\n" +
			"  │ \"func ListRepositoryCollaborators\" in *.go (~)\n" +
			"  └ Permission denied and could not request permission from user\n"
		got := detectDenials(output)
		want := []string{"Search (grep)"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("detectDenials() = %v, want %v", got, want)
		}
	})

	t.Run("multi line command", func(t *testing.T) {
		output := "✗ Inspect go-github ruleset types (shell)\n" +
			"  │ cd /repo && grep -rn\n" +
			"  │ \"BypassMode\" ./... ; echo done\n" +
			"  └ Permission denied and could not request permission from user\n"
		got := detectDenials(output)
		want := []string{"Inspect go-github ruleset types (shell)"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("detectDenials() = %v, want %v", got, want)
		}
	})

	t.Run("repeated identical message is not deduplicated", func(t *testing.T) {
		one := "✗ Delete probe file (rm)\n" +
			"  │ rm recommended/zz_probe_test.go\n" +
			"  └ Permission denied and could not request permission from user\n"
		output := strings.Repeat(one, 3)
		got := detectDenials(output)
		want := []string{"Delete probe file (rm)", "Delete probe file (rm)", "Delete probe file (rm)"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("detectDenials() = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
		}
	})

	t.Run("bare cross mark without denial message is not counted", func(t *testing.T) {
		output := "✗ Fetch (web_fetch)\n" +
			"  └ some other failure, not a permission denial\n"
		if got := detectDenials(output); len(got) != 0 {
			t.Errorf("detectDenials() = %v, want empty", got)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		if got := detectDenials(""); len(got) != 0 {
			t.Errorf("detectDenials() = %v, want empty", got)
		}
	})
}

func TestDetectDeniedCalls(t *testing.T) {
	t.Run("label tool and body", func(t *testing.T) {
		output := "✗ Inspect go-github ruleset types (shell)\n" +
			"  │ cd /repo && grep -rn\n" +
			"  │ \"BypassMode\" ./... ; echo done\n" +
			"  └ Permission denied and could not request permission from user\n"
		got := detectDeniedCalls(output)
		want := []deniedCall{{
			Label: "Inspect go-github ruleset types (shell)",
			Tool:  "shell",
			Body:  []string{"cd /repo && grep -rn", "\"BypassMode\" ./... ; echo done"},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("detectDeniedCalls() = %#v, want %#v", got, want)
		}
	})

	t.Run("label without a parenthesized tool", func(t *testing.T) {
		output := "✗ Inspect\n" +
			"  └ Permission denied and could not request permission from user\n"
		got := detectDeniedCalls(output)
		if len(got) != 1 || got[0].Label != "Inspect" || got[0].Tool != "" {
			t.Errorf("detectDeniedCalls() = %#v, want one call labelled Inspect with no tool", got)
		}
	})

	t.Run("denial far below its label is unattributed", func(t *testing.T) {
		output := "✗ Inspect (shell)\n" +
			strings.Repeat("noise\n", maxDenialLookback) +
			"  └ Permission denied and could not request permission from user\n"
		got := detectDeniedCalls(output)
		want := []deniedCall{{}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("detectDeniedCalls() = %#v, want %#v", got, want)
		}
	})
}

func TestSummarizeDenials(t *testing.T) {
	got := SummarizeDenials([]string{"a", "b", "a", "", "a"})
	want := []string{"a x3", "b", "(unknown tool)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SummarizeDenials() = %v, want %v", got, want)
	}
}

func TestNeedsToolPermissionWarning(t *testing.T) {
	tests := []struct {
		name          string
		allowAllTools bool
		extraArgs     []string
		want          bool
	}{
		{"nothing authorized", false, nil, true},
		{"allow all tools option", true, nil, false},
		{"allow-all-tools extra arg", false, []string{"--allow-all-tools"}, false},
		{"allow-all-tools= extra arg", false, []string{"--allow-all-tools=true"}, false},
		{"allow-tool extra arg", false, []string{"--allow-tool", "shell(git:*)"}, false},
		{"allow-tool= extra arg", false, []string{"--allow-tool=shell(git:*)"}, false},
		{"unrelated extra arg", false, []string{"--deny-tool", "shell(rm:*)"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsToolPermissionWarning(tt.allowAllTools, tt.extraArgs); got != tt.want {
				t.Errorf("NeedsToolPermissionWarning() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLooksLikeSlashCommand(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"slash command", "/rubber-duck foo", true},
		{"slash command with leading whitespace", "  /rubber-duck foo\nmore", true},
		{"path, not a command", "/usr/local/bin", false},
		{"plain prose", "Please judge this comment.", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LooksLikeSlashCommand(tt.prompt); got != tt.want {
				t.Errorf("LooksLikeSlashCommand(%q) = %v, want %v", tt.prompt, got, tt.want)
			}
		})
	}
}
