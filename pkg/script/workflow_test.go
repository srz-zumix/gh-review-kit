package script

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	node := &Node{Risk: "none", Content: []byte("echo hi")}
	for _, test := range []struct {
		input    string
		approved bool
	}{{"y\n", true}, {"\n", false}, {"", false}, {"v\nr\ny\n", true}, {"no\n", false}} {
		var output bytes.Buffer
		approved, err := Confirm(context.Background(), strings.NewReader(test.input), &output, node)
		if err != nil || approved != test.approved {
			t.Fatalf("%q: %t %v", test.input, approved, err)
		}
	}
}

func TestConfirmRiskThreshold(t *testing.T) {
	for _, test := range []struct {
		name     string
		risk     Risk
		limit    Risk
		input    string
		approved bool
		strong   bool
	}{
		{"below threshold", "none", "low", "y\n", true, false},
		{"at threshold", "high", "high", "yes\n", true, false},
		{"medium requires execute", "medium", "low", "execute\n", true, true},
		{"high requires execute", "high", "low", "execute\n", true, true},
		{"critical requires execute", "critical", "low", "execute\n", true, true},
		{"high rejects y", "high", "low", "y\n", false, true},
		{"high rejects yes", "high", "low", "yes\n", false, true},
		{"high rejects translated approval", "high", "low", "EXECUTE\n", false, true},
		{"high defaults to refusal", "high", "low", "\n", false, true},
		{"high refuses EOF", "high", "low", "", false, true},
		{"view and report before approval", "high", "low", "v\nr\nexecute\n", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			node := &Node{Risk: test.risk, Content: []byte("echo reviewed")}
			approved, err := confirmWithLimit(context.Background(), strings.NewReader(test.input), &output, node, test.limit)
			if err != nil || approved != test.approved {
				t.Fatalf("approved=%t error=%v", approved, err)
			}
			if strings.Contains(output.String(), "explicit approval is required") != test.strong {
				t.Fatalf("wrong confirmation: %s", output.String())
			}
		})
	}
}

func TestExecuteReviewedBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix execution")
	}
	content := []byte("#!/bin/sh\nprintf '%s' \"$1\"\nexit 7\n")
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Execute(context.Background(), path, digest(content), "", []string{"argument"}, nil, &output, &output)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 || output.String() != "argument" {
		t.Fatalf("output=%q error=%v", output.String(), err)
	}
	if err := Execute(context.Background(), path, strings.Repeat("0", 64), "", nil, nil, &output, &output); err == nil {
		t.Fatal("modified bytes accepted")
	}
}

func TestWorkflowRiskGate(t *testing.T) {
	var output bytes.Buffer
	err := Workflow(context.Background(), "-", strings.NewReader("sudo true"), &output, &output, DefaultOptions(), RunOptions{Execute: true, Yes: true, MaxRisk: "low", Format: "text"})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("risk gate: %v", err)
	}
}

func TestShellSelection(t *testing.T) {
	for _, header := range []string{"#!/usr/bin/python", "#!/bin/bash -c", "#!/usr/bin/env -S bash -c", "#!"} {
		if _, _, err := selectShell([]byte(header), ""); err == nil {
			t.Fatalf("accepted %s", header)
		}
	}
	if shell, _, err := selectShell([]byte("#!/usr/bin/env bash"), ""); err != nil || shell != "bash" {
		t.Fatalf("%s %v", shell, err)
	}
}

func TestExecuteIgnoresBashEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix execution")
	}
	directory := t.TempDir()
	envFile := filepath.Join(directory, "env.sh")
	if err := os.WriteFile(envFile, []byte("echo unreviewed"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BASH_ENV", envFile)
	content := []byte("echo reviewed")
	path := filepath.Join(directory, "script.sh")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Execute(context.Background(), path, digest(content), "bash", nil, nil, &output, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "reviewed\n" {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestRenderEscapesTerminal(t *testing.T) {
	var output bytes.Buffer
	if err := Render(&output, &Node{Source: "\x1b[2J", Risk: "none"}, "text"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") {
		t.Fatal("raw terminal escape")
	}
}

func TestRenderPreservesUnicode(t *testing.T) {
	summary := "Homebrew \u516c\u5f0f\u30a4\u30f3\u30b9\u30c8\u30fc\u30e9\u30fc"
	var output bytes.Buffer
	node := &Node{Risk: "none", AI: &AIResult{Summary: summary}}
	if err := Render(&output, node, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "AI: "+summary) {
		t.Fatalf("Unicode summary was escaped: %q", output.String())
	}
}

func TestSafeTextEscapesControls(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{"\x1b[2J", `\x1b[2J`},
		{"line\nnext\r\t\x00", `line\nnext\r\t\x00`},
		{"\u202e", `\u202e`},
		{"\u2066", `\u2066`},
	} {
		if got := safeText(test.input); got != test.want {
			t.Errorf("safeText(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestRenderGroupsFindings(t *testing.T) {
	node := &Node{Risk: "high", Findings: []Finding{
		{Line: 9, Severity: "medium", RuleID: "dynamic-command", Message: "Computed command"},
		{Line: 3, Severity: "medium", RuleID: "dynamic-command", Message: "Computed command"},
		{Line: 3, Severity: "medium", RuleID: "dynamic-command", Message: "Computed command"},
		{Line: 5, Severity: "high", RuleID: "privilege", Message: "Elevated privileges"},
		{Line: 11, Severity: "medium", RuleID: "dynamic-command", Message: "Different explanation"},
	}}
	var output bytes.Buffer
	if err := Render(&output, node, "text"); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "dynamic-command (count=3; lines=3,9): Computed command") {
		t.Fatalf("missing aggregation: %s", text)
	}
	if !strings.Contains(text, "dynamic-command (count=1; lines=11): Different explanation") {
		t.Fatalf("lost explanation: %s", text)
	}
	if strings.Index(text, "[high]") > strings.Index(text, "[medium]") {
		t.Fatalf("high risk not prioritized: %s", text)
	}
	output.Reset()
	if err := Render(&output, node, "json"); err != nil {
		t.Fatal(err)
	}
	var decoded Node
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Findings) != 5 || node.Findings[0].Line != 9 || decoded.Risk != "high" {
		t.Fatal("grouping changed raw findings or risk")
	}
}
