package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// claudeSandboxSettings enables Claude Code's sandbox; it is passed inline
// because Claude Code has no dedicated sandbox flag.
const claudeSandboxSettings = `{"sandbox":{"enabled":true}}`

// maxClaudeLabelDetail bounds how much of a denied command or path is shown in
// a denial label.
const maxClaudeLabelDetail = 100

// claudeQuotaPattern matches the messages Claude Code reports when the account
// has no usage left.
var claudeQuotaPattern = regexp.MustCompile(`(?i)usage limit|hit your (?:[a-z]+ )?limit|exceeded your (?:[a-z]+ )?quota`)

// claudePathInputKeys are the tool_input keys of a denied tool call that name a file.
var claudePathInputKeys = []string{"file_path", "path", "notebook_path"}

// claudeWriteTools are the tools a single "Edit" allow rule covers.
var claudeWriteTools = map[string]bool{
	"Edit":         true,
	"MultiEdit":    true,
	"NotebookEdit": true,
	"Write":        true,
}

// claudeDenial is an entry of the permission_denials list of a Claude Code result.
type claudeDenial struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// claudeResult is the final "result" message of a Claude Code -p run.
type claudeResult struct {
	Type              string         `json:"type"`
	IsError           bool           `json:"is_error"`
	Result            string         `json:"result"`
	TotalCostUSD      float64        `json:"total_cost_usd"`
	PermissionDenials []claudeDenial `json:"permission_denials"`
}

// buildClaudeArgs assembles the Claude Code invocation arguments. The prompt
// is not among them: it is piped through stdin, which avoids argument length
// limits and a prompt that starts with a dash being read as an option.
func buildClaudeArgs(opts EvaluateOptions) []string {
	// stream-json reports progress as it happens; -p requires --verbose for it.
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if opts.Agent != "" {
		args = append(args, "--agent", opts.Agent)
	}
	if opts.SessionID != "" {
		if opts.ResumeSession {
			args = append(args, "--resume", opts.SessionID)
		} else {
			args = append(args, "--session-id", opts.SessionID)
		}
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.AutoApprove {
		args = append(args, "--permission-mode", "auto")
	}
	if opts.Sandbox {
		args = append(args, "--settings", claudeSandboxSettings)
	}
	args = append(args, opts.ExtraArgs...)
	return args
}

// runClaudeCLI invokes Claude Code with prompt and opts. Its stdout is a stream
// of JSON messages rather than a transcript, so the messages are rendered to
// opts.Log as they arrive and the final result text is returned for parsing.
func runClaudeCLI(ctx context.Context, opts EvaluateOptions, prompt string) (cliRun, error) {
	var progress *claudeStreamLog
	var progressW io.Writer
	if opts.Log != nil {
		progress = &claudeStreamLog{w: opts.Log, lastEvent: time.Now()}
		progressW = progress
	}
	var done chan struct{}
	if progress != nil {
		done = make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					progress.reportWait()
				case <-done:
					return
				}
			}
		}()
	}
	out, err := runProcess(ctx, opts, buildClaudeArgs(opts), prompt, false, progressW)
	if progress != nil {
		close(done)
		progress.Flush()
	}
	res, ok := parseClaudeResult(out.Stdout)
	if !ok {
		return cliRun{Output: strings.TrimSpace(out.Stdout + "\n" + out.Stderr)}, err
	}
	return cliRun{Output: res.Result, Usage: newClaudeUsage(opts, res)}, err
}

// claudeStreamMessage is one line of Claude Code's stream-json output.
type claudeStreamMessage struct {
	Type    string          `json:"type"`
	IsError bool            `json:"is_error"`
	Result  string          `json:"result"`
	Message json.RawMessage `json:"message"`
}

// claudeAssistantMessage is the message of an "assistant" stream-json line.
type claudeAssistantMessage struct {
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"content"`
}

// claudeStreamLog renders Claude Code's stream-json output to w line by line,
// showing the assistant's text and tool calls.
type claudeStreamLog struct {
	mu        sync.Mutex
	w         io.Writer
	buf       []byte
	lastEvent time.Time
}

func (l *claudeStreamLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastEvent = time.Now()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.render(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

// Flush renders a final line that was not terminated by a newline.
func (l *claudeStreamLog) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.render(l.buf)
	l.buf = nil
}

func (l *claudeStreamLog) reportWait() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.lastEvent) >= time.Minute {
		fmt.Fprintln(l.w, "Waiting for Claude Code (no output for at least 1 minute)")
	}
}

func (l *claudeStreamLog) render(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var m claudeStreamMessage
	if json.Unmarshal(line, &m) != nil {
		fmt.Fprintf(l.w, "%s\n", line)
		return
	}
	switch m.Type {
	case "assistant":
		var msg claudeAssistantMessage
		if json.Unmarshal(m.Message, &msg) != nil {
			return
		}
		for _, c := range msg.Content {
			switch c.Type {
			case "text":
				if text := strings.TrimSpace(c.Text); text != "" {
					fmt.Fprintln(l.w, text)
				}
			case "tool_use":
				fmt.Fprintf(l.w, "● %s\n", claudeToolLabel(c.Name, c.Input))
			}
		}
	case "user":
		var msg struct {
			Content []struct {
				Type    string `json:"type"`
				IsError bool   `json:"is_error"`
			} `json:"content"`
		}
		if json.Unmarshal(m.Message, &msg) != nil {
			return
		}
		for _, content := range msg.Content {
			if content.Type == "tool_result" {
				if content.IsError {
					fmt.Fprintln(l.w, "● tool failed")
				} else {
					fmt.Fprintln(l.w, "● tool completed")
				}
			}
		}
	case "result":
		// A successful result repeats the last assistant text, which is already shown.
		if m.IsError {
			fmt.Fprintln(l.w, m.Result)
		}
	}
}

// claudeToolLabel renders a tool call as "Tool(detail)", with the command of a
// Bash call or the files a tool works on as the detail.
func claudeToolLabel(tool string, input map[string]any) string {
	if tool == "Bash" {
		return claudeDenialLabel(tool, inputString(input, "command"))
	}
	var paths []string
	for _, key := range claudePathInputKeys {
		if p := inputString(input, key); p != "" {
			paths = append(paths, p)
		}
	}
	return claudeDenialLabel(tool, strings.Join(paths, ", "))
}

// parseClaudeResult finds the "result" message in Claude Code's stdout, which
// is either a single JSON object, an array of messages, or one message per line.
func parseClaudeResult(stdout string) (*claudeResult, bool) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil, false
	}
	var single claudeResult
	if strings.HasPrefix(trimmed, "{") && json.Unmarshal([]byte(trimmed), &single) == nil && single.Type == "result" {
		return &single, true
	}
	var list []claudeResult
	if strings.HasPrefix(trimmed, "[") && json.Unmarshal([]byte(trimmed), &list) == nil {
		for i := len(list) - 1; i >= 0; i-- {
			if list[i].Type == "result" {
				return &list[i], true
			}
		}
		return nil, false
	}
	lines := strings.Split(trimmed, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var r claudeResult
		if json.Unmarshal([]byte(line), &r) == nil && r.Type == "result" {
			return &r, true
		}
	}
	return nil, false
}

// newClaudeUsage builds a Usage from a Claude Code result, returning nil when
// there is nothing to report.
func newClaudeUsage(opts EvaluateOptions, res *claudeResult) *Usage {
	calls := claudeDeniedCalls(res.PermissionDenials)
	quotaExceeded := res.IsError && claudeQuotaPattern.MatchString(res.Result)
	if res.TotalCostUSD == 0 && len(calls) == 0 && !quotaExceeded {
		return nil
	}
	usage := &Usage{CostUSD: res.TotalCostUSD, QuotaExceeded: quotaExceeded}
	usage.Denials = denialLabels(calls)
	usage.Recommendations, usage.WritablePaths = recommendClaudePermissions(opts, calls)
	return usage
}

// claudeDeniedCalls converts Claude Code's permission denials to deniedCalls,
// presenting Bash as the shell tool so that the shell analysis is shared with
// the Copilot CLI.
func claudeDeniedCalls(denials []claudeDenial) []deniedCall {
	calls := make([]deniedCall, 0, len(denials))
	for _, d := range denials {
		if d.ToolName == "Bash" {
			command := inputString(d.ToolInput, "command")
			call := deniedCall{Label: claudeDenialLabel(d.ToolName, command), Tool: "shell"}
			if command != "" {
				call.Body = []string{command}
			}
			calls = append(calls, call)
			continue
		}
		var paths []string
		for _, key := range claudePathInputKeys {
			if p := inputString(d.ToolInput, key); p != "" {
				paths = append(paths, p)
			}
		}
		calls = append(calls, deniedCall{
			Label: claudeDenialLabel(d.ToolName, strings.Join(paths, ", ")),
			Tool:  d.ToolName,
			Paths: paths,
		})
	}
	return calls
}

// inputString returns the string value of key in a tool_input object.
func inputString(input map[string]any, key string) string {
	s, _ := input[key].(string)
	return s
}

// claudeDenialLabel renders a denied tool call as "Tool(detail)".
func claudeDenialLabel(tool, detail string) string {
	detail = strings.Join(strings.Fields(detail), " ")
	if detail == "" {
		return tool
	}
	if r := []rune(detail); len(r) > maxClaudeLabelDetail {
		detail = string(r[:maxClaudeLabelDetail]) + "..."
	}
	return tool + "(" + detail + ")"
}

// claudeExistingOptions is the permission setup an evaluation already used, so
// that recommendations do not repeat it.
type claudeExistingOptions struct {
	// bypass is set when permission checks are skipped altogether.
	bypass bool
	// auto is set when the permission mode is auto.
	auto  bool
	rules map[string]bool
	dirs  []string
}

// existingClaudeOptions reads the permission options out of opts, including
// the variadic --allowedTools and --add-dir forms.
func existingClaudeOptions(opts EvaluateOptions) claudeExistingOptions {
	existing := claudeExistingOptions{rules: map[string]bool{}}
	// The --permission-mode emitted for AutoApprove precedes ExtraArgs, so a
	// later --permission-mode overrides it; the last one is the effective mode.
	mode := ""
	if opts.AutoApprove {
		mode = "auto"
	}
	skipPermissions := false
	args := opts.ExtraArgs
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "--dangerously-skip-permissions":
			skipPermissions = true
		case "--permission-mode":
			if !hasValue && i+1 < len(args) {
				i++
				value = args[i]
			}
			mode = value
		case "--allowedTools", "--allowed-tools", "--add-dir":
			var values []string
			if hasValue {
				values = append(values, value)
			}
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				values = append(values, args[i])
			}
			for _, v := range values {
				if name == "--add-dir" {
					existing.dirs = append(existing.dirs, strings.TrimSuffix(v, "/"))
					continue
				}
				for _, rule := range splitClaudeRules(v) {
					existing.rules[rule] = true
				}
			}
		}
	}
	existing.auto = mode == "auto"
	existing.bypass = skipPermissions || mode == "bypassPermissions"
	return existing
}

// splitClaudeRules splits a comma- or space-separated rule list, keeping
// separators inside parentheses, e.g. "Bash(git log *),Read".
func splitClaudeRules(list string) []string {
	var rules []string
	var current strings.Builder
	depth := 0
	flush := func() {
		if rule := strings.TrimSpace(current.String()); rule != "" {
			rules = append(rules, rule)
		}
		current.Reset()
	}
	for _, r := range list {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case (r == ',' || r == ' ') && depth == 0:
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return rules
}

// coveredByAny reports whether any of parents grants dir.
func coveredByAny(parents []string, dir string) bool {
	for _, p := range parents {
		if covers(p, dir) {
			return true
		}
	}
	return false
}

// recommendClaudePermissions returns the Claude Code options that would have
// allowed the denied calls, along with the directories the calls were going to
// write to. Tool rules come first, then directories; options already in effect
// are omitted.
func recommendClaudePermissions(opts EvaluateOptions, calls []deniedCall) (options []string, writable []string) {
	if len(calls) == 0 {
		return nil, nil
	}
	existing := existingClaudeOptions(opts)
	if existing.bypass {
		return nil, nil
	}
	granted, _ := os.Getwd()

	var rules, dirs []string
	fallback := false
	for _, c := range calls {
		if shellToolNames[strings.ToLower(c.Tool)] {
			commands := deniedCommands(c)
			if len(commands) == 0 {
				fallback = true
			}
			for _, name := range commands {
				rules = appendUnique(rules, []string{"Bash(" + name + " *)"})
			}
			d, w := callPathGrants(c, granted)
			dirs = append(dirs, d...)
			writable = append(writable, w...)
			continue
		}
		var grants []string
		for _, p := range c.Paths {
			if dir := pathGrant(p); dir != "" && !covers(granted, dir) {
				grants = append(grants, dir)
			}
		}
		dirs = append(dirs, grants...)
		switch {
		case claudeWriteTools[c.Tool]:
			rules = appendUnique(rules, []string{"Edit"})
			writable = append(writable, grants...)
		case len(grants) == 0 && c.Tool != "":
			rules = appendUnique(rules, []string{c.Tool})
		}
	}

	sort.Strings(rules)
	for _, rule := range rules {
		if existing.rules[rule] || (strings.HasPrefix(rule, "Bash(") && existing.rules["Bash"]) {
			continue
		}
		options = append(options, "--allowedTools="+rule)
	}
	if fallback && !existing.auto {
		options = append(options, "--permission-mode=auto")
	}
	for _, dir := range collapseDirs(dirs) {
		if !coveredByAny(existing.dirs, dir) {
			options = append(options, "--add-dir="+dir)
		}
	}
	return options, collapseDirs(writable)
}
