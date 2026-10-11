package script

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/guardrails"
	"golang.org/x/term"
)

type RunOptions struct {
	Execute bool
	Yes     bool
	MaxRisk string
	Shell   string
	Args    []string
	Format  string
	Save    string
}

func ValidateRunOptions(opts RunOptions) error {
	if opts.Format != "text" && opts.Format != "json" {
		return fmt.Errorf("format must be text or json")
	}
	if _, err := ParseRisk(opts.MaxRisk); err != nil {
		return err
	}
	if opts.Shell != "" && !isShell(opts.Shell) {
		return fmt.Errorf("shell must be sh, bash, dash, zsh, or ksh (optionally an absolute path)")
	}
	if opts.Shell != "" && strings.Contains(opts.Shell, "/") && !filepath.IsAbs(opts.Shell) {
		return fmt.Errorf("shell path must be absolute")
	}
	if opts.Execute && guardrails.IsReadonly() {
		return fmt.Errorf("script run cannot execute in read-only mode")
	}
	return nil
}

func Workflow(ctx context.Context, source string, stdin io.Reader, stdout, stderr io.Writer, opts Options, run RunOptions) error {
	if err := ValidateRunOptions(run); err != nil {
		return err
	}
	if err := ValidateOptions(opts); err != nil {
		return err
	}
	directory, err := os.MkdirTemp("", "gh-review-kit-script-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	opts.WorkDir = directory
	node, err := Review(ctx, source, stdin, opts)
	if err != nil {
		return fmt.Errorf("review script: %w", err)
	}
	path := filepath.Join(directory, "script.sh")
	if err := os.WriteFile(path, node.Content, 0600); err != nil {
		return err
	}
	if run.Save != "" {
		if guardrails.IsReadonly() {
			return fmt.Errorf("saving artifacts is unavailable in read-only mode")
		}
		if err := Save(node, run.Save); err != nil {
			return fmt.Errorf("save review: %w", err)
		}
	}
	reportOutput := stdout
	if run.Execute {
		reportOutput = stderr
	}
	if err := Render(reportOutput, node, run.Format); err != nil {
		return err
	}
	if !run.Execute {
		return nil
	}
	limit, _ := ParseRisk(run.MaxRisk)
	if run.Yes && rank(node.Risk) > rank(limit) {
		return fmt.Errorf("risk %s exceeds --max-risk %s; script was not executed", node.Risk, limit)
	}
	var tty *os.File
	if file, openErr := os.OpenFile("/dev/tty", os.O_RDWR, 0); openErr == nil {
		if term.IsTerminal(int(file.Fd())) {
			tty = file
		} else {
			_ = file.Close()
		}
	}
	if tty != nil {
		defer tty.Close()
	}
	if !run.Yes {
		if tty == nil {
			return fmt.Errorf("no terminal for confirmation; use --yes with --max-risk")
		}
		approved, err := confirmWithLimit(ctx, tty, tty, node, limit)
		if err != nil {
			return err
		}
		if !approved {
			return fmt.Errorf("execution cancelled")
		}
	}
	execInput := stdin
	if source == "-" && tty != nil {
		execInput = tty
	}
	return Execute(ctx, path, node.SHA256, run.Shell, run.Args, execInput, stdout, stderr)
}

func safeText(value string) string {
	quoted := strconv.Quote(value)
	return quoted[1 : len(quoted)-1]
}

func Render(writer io.Writer, node *Node, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(node)
	}
	var report strings.Builder
	report.WriteString("Review is heuristic, not proof of safety. Execution is NOT sandboxed.\n")
	var renderNode func(*Node, string)
	renderNode = func(current *Node, indent string) {
		fmt.Fprintf(&report, "%sSource: %s\n%sSHA256: %s\n%sRisk: %s\n", indent, safeText(current.Source), indent, current.SHA256, indent, current.Risk)
		writeFindingGroups(&report, current.Findings, indent)
		for _, download := range current.Downloads {
			fmt.Fprintf(&report, "%s  Download (script=%t): %s\n", indent, download.Script, safeText(download.URL))
		}
		if current.AI != nil {
			fmt.Fprintf(&report, "%s  AI: %s\n", indent, safeText(current.AI.Summary))
		}
		for _, child := range current.Children {
			renderNode(child, indent+"  ")
		}
	}
	renderNode(node, "")
	_, err := io.WriteString(writer, report.String())
	return err
}

func writeFindingGroups(report *strings.Builder, findings []Finding, indent string) {
	type groupKey struct {
		severity Risk
		rule     string
		message  string
	}
	type group struct {
		key   groupKey
		count int
		lines []int
	}
	var groups []group
	indices := make(map[groupKey]int)
	for _, finding := range findings {
		key := groupKey{finding.Severity, finding.RuleID, finding.Message}
		index, exists := indices[key]
		if !exists {
			index = len(groups)
			indices[key] = index
			groups = append(groups, group{key: key})
		}
		groups[index].count++
		groups[index].lines = append(groups[index].lines, finding.Line)
	}
	slices.SortStableFunc(groups, func(left, right group) int { return rank(right.key.severity) - rank(left.key.severity) })
	for _, current := range groups {
		slices.Sort(current.lines)
		var lines []string
		for _, line := range slices.Compact(current.lines) {
			if line == 0 {
				lines = append(lines, "script-wide")
			} else {
				lines = append(lines, strconv.Itoa(line))
			}
		}
		fmt.Fprintf(report, "%s  [%s] %s (count=%d; lines=%s): %s\n", indent, current.key.severity, safeText(current.key.rule), current.count, strings.Join(lines, ","), safeText(current.key.message))
	}
}

func Save(node *Node, directory string) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	var saveNode func(*Node) error
	saveNode = func(current *Node) error {
		path := filepath.Join(directory, current.SHA256+".sh")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil && !os.IsExist(err) {
			return err
		}
		if err == nil {
			_, writeErr := file.Write(current.Content)
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		for _, child := range current.Children {
			if err := saveNode(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := saveNode(node); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, "report.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	writeErr := Render(file, node, "json")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func Confirm(ctx context.Context, input io.Reader, output io.Writer, node *Node) (bool, error) {
	return confirmWithLimit(ctx, input, output, node, "low")
}

func confirmWithLimit(ctx context.Context, input io.Reader, output io.Writer, node *Node, limit Risk) (bool, error) {
	strong := rank(node.Risk) > rank(limit)
	if strong {
		if _, err := fmt.Fprintf(output, "Risk %s exceeds --max-risk %s. Execution is NOT sandboxed; explicit approval is required.\n", node.Risk, limit); err != nil {
			return false, err
		}
	}
	reader := bufio.NewReader(input)
	for {
		prompt := "Execute reviewed script? [y/N/v(iew)/r(eport)] "
		if strong {
			prompt = "Execute reviewed script? Type execute to approve [execute/N/v(iew)/r(eport)] "
		}
		if _, err := fmt.Fprint(output, prompt); err != nil {
			return false, err
		}
		answer, err := reader.ReadString('\n')
		if err != nil {
			return false, nil
		}
		if strong && strings.TrimSpace(answer) == "execute" {
			return true, nil
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return !strong, nil
		case "v", "view":
			var text strings.Builder
			for index, line := range strings.Split(string(node.Content), "\n") {
				fmt.Fprintf(&text, "%5d  %s\n", index+1, safeText(line))
			}
			if _, ok := input.(*os.File); ok {
				pager := os.Getenv("PAGER")
				if pager == "" {
					pager = "less -F -X"
				}
				args, err := shellquote.Split(pager)
				if err != nil || len(args) == 0 {
					return false, fmt.Errorf("invalid PAGER")
				}
				cmd := exec.CommandContext(ctx, args[0], args[1:]...)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(text.String()), output, output
				if err := cmd.Run(); err != nil {
					return false, fmt.Errorf("view script: %w", err)
				}
			} else if _, err := io.WriteString(output, text.String()); err != nil {
				return false, err
			}
		case "r", "report":
			if err := Render(output, node, "text"); err != nil {
				return false, err
			}
		default:
			return false, nil
		}
	}
}

func selectShell(content []byte, override string) (string, []string, error) {
	if override != "" {
		return override, nil, nil
	}
	first, _, _ := bytes.Cut(content, []byte("\n"))
	if !bytes.HasPrefix(first, []byte("#!")) {
		return "bash", nil, nil
	}
	fields := strings.Fields(strings.TrimSpace(string(first[2:])))
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("empty shebang")
	}
	if fields[0] == "/usr/bin/env" || fields[0] == "/bin/env" {
		fields = fields[1:]
		if len(fields) > 0 && fields[0] == "-S" {
			fields = fields[1:]
		}
	}
	if len(fields) == 0 || !isShell(fields[0]) {
		return "", nil, fmt.Errorf("unsupported shebang; select --shell explicitly")
	}
	for _, flag := range fields[1:] {
		if flag != "-e" && flag != "-u" && flag != "-x" && flag != "-eu" && flag != "-ue" {
			return "", nil, fmt.Errorf("unsupported shebang option %q; select --shell explicitly", flag)
		}
	}
	return filepath.Base(fields[0]), fields[1:], nil
}

func Execute(ctx context.Context, path, expected, shell string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("script execution requires Unix")
	}
	if guardrails.IsReadonly() {
		return fmt.Errorf("script execution is unavailable in read-only mode")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	content, err := readLimited(file, 64<<20)
	if err != nil {
		return err
	}
	if digest(content) != expected {
		return fmt.Errorf("reviewed script changed; execution refused")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	interpreter, flags, err := selectShell(content, shell)
	if err != nil {
		return err
	}
	if !isShell(interpreter) {
		return fmt.Errorf("unsupported shell")
	}
	commandArgs := append(flags, "/dev/fd/3")
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, interpreter, commandArgs...)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if name == "BASH_ENV" || name == "ENV" || name == "SHELLOPTS" || name == "BASHOPTS" || name == "ZDOTDIR" || strings.HasPrefix(name, "BASH_FUNC_") {
			continue
		}
		cmd.Env = append(cmd.Env, value)
	}
	cmd.ExtraFiles = []*os.File{file}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}
