// Package copilot supports fetching Copilot code review comments, judging
// them with the Copilot CLI, and acting on the verdict (feedback reaction or
// thread resolution).
package copilot

import "time"

// DefaultAuthor is the bot login used by GitHub Copilot code review. The
// GraphQL API returns this login without the "[bot]" suffix used elsewhere.
const DefaultAuthor = "copilot-pull-request-reviewer"

// ReviewerLogin is the login used to request a review from GitHub Copilot
// through the REST "request reviewers" API, which requires the "[bot]" suffix.
const ReviewerLogin = "copilot-pull-request-reviewer[bot]"

// Verdict is the outcome of judging whether a Comment's feedback is correct.
type Verdict string

const (
	VerdictValid   Verdict = "valid"
	VerdictInvalid Verdict = "invalid"
	VerdictUnclear Verdict = "unclear"
)

// Verdicts lists all valid Verdict values.
var Verdicts = []string{string(VerdictValid), string(VerdictInvalid), string(VerdictUnclear)}

// Comment is a Copilot-authored review comment on a pull request, flattened
// together with its review thread's resolution state.
type Comment struct {
	ThreadID   string    `json:"thread_id"`
	CommentID  int64     `json:"comment_id"`
	URL        string    `json:"url"`
	Author     string    `json:"author"`
	Body       string    `json:"body"`
	Path       string    `json:"path"`
	Line       int       `json:"line"`
	DiffHunk   string    `json:"diff_hunk"`
	IsResolved bool      `json:"is_resolved"`
	IsOutdated bool      `json:"is_outdated"`
	CreatedAt  time.Time `json:"created_at"`
}

// Evaluation is the structured result the Copilot CLI is asked to produce
// when judging whether a Comment's feedback is correct.
type Evaluation struct {
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
}

// EvaluationResult pairs a Comment with its Evaluation and the Action taken on it.
type EvaluationResult struct {
	Comment    *Comment    `json:"comment"`
	Evaluation *Evaluation `json:"evaluation,omitempty"`
	Action     Action      `json:"action"`
	// Denials lists tool calls the Copilot CLI denied while producing
	// Evaluation. The same list may appear on multiple results when they
	// share a single Copilot CLI invocation (e.g. under --batch).
	Denials []string `json:"denials,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Usage is the resource consumption the Copilot CLI reports for an evaluation
// session. The Copilot CLI reports cumulative session totals, so the Usage of
// the most recent evaluation covers every evaluation of that session.
type Usage struct {
	AICredits float64 `json:"ai_credits"`
	// Denials lists tool calls the Copilot CLI denied, one entry per
	// occurrence (not deduplicated). Unlike AICredits, denials are not
	// reported cumulatively by the Copilot CLI and must be aggregated by callers.
	Denials []string `json:"denials,omitempty"`
	// Recommendations lists the Copilot CLI options that would have allowed
	// the denied tool calls, most specific first.
	Recommendations []string `json:"recommendations,omitempty"`
	// QuotaExceeded reports that the Copilot CLI stopped because the account
	// ran out of quota, which leaves the evaluation unfinished.
	QuotaExceeded bool `json:"quota_exceeded,omitempty"`
}
