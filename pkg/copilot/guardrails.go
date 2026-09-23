package copilot

import (
	"fmt"
	"regexp"
	"strings"
)

// deniedMessage is the exact, literal text the Copilot CLI prints for every
// tool call it denies because -p mode cannot prompt the user for permission.
const deniedMessage = "Permission denied and could not request permission from user"

// maxDenialLookback bounds how far above a denial message its introducing
// "✗ <label>" line may be before the two are considered unrelated.
const maxDenialLookback = 20

// quotaExceededPattern matches the message the Copilot CLI prints when the
// account has no quota left, which aborts the run wherever it happens to be.
var quotaExceededPattern = regexp.MustCompile(`(?i)exceeded your (?:[a-z]+ )?quota`)

// detectQuotaExceeded reports whether output shows the Copilot CLI running out
// of quota.
func detectQuotaExceeded(output string) bool {
	return quotaExceededPattern.MatchString(output)
}

// deniedCall is a tool call the Copilot CLI denied. The Copilot CLI renders
// one as a "✗ <label> (<tool>)" line, the command body on "│" continuation
// lines, and the denial message on a "└" line.
type deniedCall struct {
	Label string
	Tool  string
	Body  []string
}

// detectDeniedCalls scans output for tool calls the Copilot CLI denied. It is
// intentionally not deduplicated: the same tool can be denied repeatedly
// (e.g. an agent retrying a blocked command), and the count is significant.
func detectDeniedCalls(output string) []deniedCall {
	var calls []deniedCall
	var pending deniedCall
	start := -1
	for i, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "✗ "):
			label := strings.TrimPrefix(trimmed, "✗ ")
			pending = deniedCall{Label: label, Tool: toolDisplayName(label)}
			start = i
		case strings.HasPrefix(trimmed, "│") && start >= 0:
			pending.Body = append(pending.Body, strings.TrimSpace(strings.TrimPrefix(trimmed, "│")))
		}
		if !strings.Contains(line, deniedMessage) {
			continue
		}
		if start < 0 || i-start > maxDenialLookback {
			calls = append(calls, deniedCall{})
			continue
		}
		calls = append(calls, pending)
	}
	return calls
}

// toolDisplayName extracts the name the Copilot CLI appends in parentheses to
// a tool call's label, e.g. "Search (grep)" yields "grep".
func toolDisplayName(label string) string {
	if !strings.HasSuffix(label, ")") {
		return ""
	}
	i := strings.LastIndex(label, "(")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(label[i+1 : len(label)-1])
}

// denialLabels projects denied calls onto their labels, for callers that only
// report which tool calls were denied.
func denialLabels(calls []deniedCall) []string {
	if len(calls) == 0 {
		return nil
	}
	labels := make([]string, 0, len(calls))
	for _, c := range calls {
		labels = append(labels, c.Label)
	}
	return labels
}

// SummarizeDenials groups denials (as returned by Usage.Denials) by label,
// preserving first-seen order, and formats each group as "label" or
// "label xN" for display; an empty label is rendered as "(unknown tool)".
func SummarizeDenials(denials []string) []string {
	order := make([]string, 0, len(denials))
	counts := make(map[string]int, len(denials))
	for _, d := range denials {
		if _, ok := counts[d]; !ok {
			order = append(order, d)
		}
		counts[d]++
	}
	summary := make([]string, 0, len(order))
	for _, label := range order {
		display := label
		if display == "" {
			display = "(unknown tool)"
		}
		if n := counts[label]; n > 1 {
			display = fmt.Sprintf("%s x%d", display, n)
		}
		summary = append(summary, display)
	}
	return summary
}

// NeedsToolPermissionWarning reports whether an evaluation invocation is
// likely to have tool calls denied because no permission was pre-authorized:
// allowAllTools is false and extraArgs contains neither --allow-all-tools nor
// --allow-tool (with or without an "=value" suffix).
func NeedsToolPermissionWarning(allowAllTools bool, extraArgs []string) bool {
	if allowAllTools {
		return false
	}
	for _, a := range extraArgs {
		switch {
		case a == "--allow-all-tools", strings.HasPrefix(a, "--allow-all-tools="):
			return false
		case a == "--allow-tool", strings.HasPrefix(a, "--allow-tool="):
			return false
		}
	}
	return true
}

// slashCommandPattern matches a leading interactive-mode slash command, e.g.
// "/rubber-duck ...", while avoiding false positives like "/usr/local/bin".
var slashCommandPattern = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9-]*(\s|$)`)

// LooksLikeSlashCommand reports whether prompt's first line looks like an
// interactive-mode slash command. The Copilot CLI's -p mode never expands
// slash commands: it passes them through as literal text, so a prompt
// starting with one is likely a mistake.
func LooksLikeSlashCommand(prompt string) bool {
	trimmed := strings.TrimSpace(prompt)
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		trimmed = trimmed[:i]
	}
	return slashCommandPattern.MatchString(trimmed)
}
