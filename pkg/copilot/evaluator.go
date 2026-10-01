package copilot

// Evaluator identifies the agentic CLI used to judge review comments.
type Evaluator string

const (
	// EvaluatorCopilot runs the GitHub Copilot CLI. It is the default.
	EvaluatorCopilot Evaluator = "copilot"
	// EvaluatorClaude runs Claude Code.
	EvaluatorClaude Evaluator = "claude"
)

// Evaluators lists all valid Evaluator values.
var Evaluators = []string{string(EvaluatorCopilot), string(EvaluatorClaude)}

// IsClaude reports whether e is Claude Code; the zero value means Copilot.
func (e Evaluator) IsClaude() bool {
	return e == EvaluatorClaude
}

// DefaultBin returns the executable name used when no binary is configured.
func (e Evaluator) DefaultBin() string {
	if e.IsClaude() {
		return "claude"
	}
	return "copilot"
}

// DisplayName returns the CLI's name as shown to users.
func (e Evaluator) DisplayName() string {
	if e.IsClaude() {
		return "Claude Code"
	}
	return "Copilot CLI"
}
