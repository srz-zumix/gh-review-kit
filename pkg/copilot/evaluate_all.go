package copilot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// EvaluateAll judges every comment of a pull request and applies each
// verdict (see ApplyEvaluation), either with one Copilot CLI invocation per
// comment (batch is false) or a single invocation covering every comment
// (batch is true). It returns one EvaluationResult per comment, in the same
// order as comments, plus the most recent Usage the Copilot CLI reported. The
// returned error is non-nil when any comment failed to be evaluated or acted
// on; the per-comment details are retained in the results so callers can
// still render every outcome.
func EvaluateAll(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, opts EvaluateOptions, repoSlug string, prNumber int, comments []*Comment, batch bool, dryRun bool) ([]*EvaluationResult, *Usage, error) {
	var results []*EvaluationResult
	var usage *Usage
	if batch {
		results, usage = evaluateAllBatch(ctx, opts, repoSlug, prNumber, comments)
	} else {
		results, usage = evaluateAllSequential(ctx, opts, repoSlug, prNumber, comments)
	}

	for _, res := range results {
		if res.Evaluation == nil {
			continue
		}
		action, err := ApplyEvaluation(ctx, g, repo, res.Comment, res.Evaluation, dryRun)
		res.Action = action
		if err != nil {
			res.Error = err.Error()
		}
	}
	return results, usage, aggregateErrors(results)
}

// aggregateErrors returns a single error summarizing every comment that
// failed to be evaluated or acted on, or nil when all results succeeded. A
// result with neither an Evaluation nor a recorded Error is treated as a
// failure, since a comment must always yield a verdict or an explanation.
func aggregateErrors(results []*EvaluationResult) error {
	var failed []string
	for _, res := range results {
		if res.Evaluation == nil && res.Error == "" {
			res.Error = "no evaluation was produced"
		}
		if res.Error != "" {
			failed = append(failed, fmt.Sprintf("comment %d: %s", res.Comment.CommentID, res.Error))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("%d of %d comments failed to evaluate: %s", len(failed), len(results), strings.Join(failed, "; "))
}

// evaluateAllSequential runs one Copilot CLI invocation per comment.
func evaluateAllSequential(ctx context.Context, opts EvaluateOptions, repoSlug string, prNumber int, comments []*Comment) ([]*EvaluationResult, *Usage) {
	results := make([]*EvaluationResult, 0, len(comments))
	var usage *Usage
	var allDenials []string
	var allRecommendations []string
	var allWritablePaths []string
	var quotaExceeded bool
	for _, c := range comments {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, "\n--- evaluating comment %d (%s) ---\n", c.CommentID, c.URL)
		}
		res := &EvaluationResult{Comment: c}
		eval, u, err := Evaluate(ctx, opts, repoSlug, prNumber, c)
		// Claude Code rejects --session-id for a session that already exists, so
		// every invocation after the first has to resume it.
		opts.ResumeSession = true
		if u != nil {
			usage = u
			res.Denials = u.Denials
			allDenials = append(allDenials, u.Denials...)
			allRecommendations = appendUnique(allRecommendations, u.Recommendations)
			allWritablePaths = appendUnique(allWritablePaths, u.WritablePaths)
			quotaExceeded = quotaExceeded || u.QuotaExceeded
		}
		if err != nil {
			res.Error = err.Error()
			results = append(results, res)
			continue
		}
		res.Evaluation = eval
		results = append(results, res)
	}
	if usage != nil {
		// The Copilot CLI reports denials per invocation, not cumulatively, so
		// the session-level Usage must aggregate them across every invocation.
		usage.Denials = allDenials
		usage.Recommendations = allRecommendations
		usage.WritablePaths = allWritablePaths
		usage.QuotaExceeded = quotaExceeded
	}
	return results, usage
}

// appendUnique appends the values not already in dst, preserving first-seen order.
func appendUnique(dst []string, values []string) []string {
	for _, v := range values {
		if !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}

// evaluateAllBatch runs a single Copilot CLI invocation covering every comment.
func evaluateAllBatch(ctx context.Context, opts EvaluateOptions, repoSlug string, prNumber int, comments []*Comment) ([]*EvaluationResult, *Usage) {
	if opts.SessionID == "" {
		opts.SessionID = NewSessionID()
	}
	perCommentTimeout := opts.Timeout
	opts.Timeout = batchTimeout(opts.Timeout, len(comments))
	if opts.Log != nil {
		fmt.Fprintf(opts.Log, "\n--- evaluating %d comments in a single batch (timeout %s) ---\n", len(comments), opts.Timeout)
	}
	evals, usage, err := EvaluateBatch(ctx, opts, repoSlug, prNumber, comments)
	var results []*EvaluationResult
	if err != nil {
		results = make([]*EvaluationResult, len(comments))
		for i, c := range comments {
			results[i] = &EvaluationResult{Comment: c, Error: err.Error()}
		}
		// A response that parsed but covered none of the comments is the
		// all-missing case of the same defect recovery handles, so it is
		// retried too; a genuine execution or JSON failure is not.
		if !errors.Is(err, ErrNoVerdicts) {
			return withDenials(results, usage), usage
		}
	} else {
		results = assignBatchResults(comments, evals)
	}
	var pending []*Comment
	for _, res := range results {
		if res.Evaluation == nil {
			pending = append(pending, res.Comment)
		}
	}
	if len(pending) == 0 || ctx.Err() != nil || (usage != nil && usage.QuotaExceeded) {
		return withDenials(results, usage), usage
	}

	opts.ResumeSession = true
	opts.Timeout = batchTimeout(perCommentTimeout, len(pending))
	opts.RubberDuck = false
	// The first pass may already have applied fixes, so the recovery must not
	// act on the repository whatever permissions that pass was granted.
	opts.ReadOnly = true
	opts.Prompt = "Your previous batch response had missing or duplicate comment IDs. Recover the judgements for only the comments listed below from the work already completed in this session. Do not edit files, run tools, or repeat fixes. Return each listed comment_id exactly once with its verdict and reason in the final JSON array. If a judgement cannot be recovered confidently, use unclear and explain why. Do not include any other comment IDs or corrections outside the JSON block."
	if opts.Log != nil {
		fmt.Fprintf(opts.Log, "\n--- recovering evaluations for %d missing or duplicate comments (timeout %s) ---\n", len(pending), opts.Timeout)
	}
	recovered, retryUsage, retryErr := EvaluateBatch(ctx, opts, repoSlug, prNumber, pending)
	if retryUsage != nil {
		if usage != nil {
			retryUsage.Denials = append(usage.Denials, retryUsage.Denials...)
			retryUsage.Recommendations = appendUnique(usage.Recommendations, retryUsage.Recommendations)
			retryUsage.WritablePaths = appendUnique(usage.WritablePaths, retryUsage.WritablePaths)
			retryUsage.QuotaExceeded = usage.QuotaExceeded || retryUsage.QuotaExceeded
			// A retry that ended before the CLI printed its usage footer (a
			// quota stop, for instance) reports zero counters, which must not
			// erase what the first invocation already reported.
			keepCounters(retryUsage, usage)
		}
		usage = retryUsage
	}
	for _, res := range results {
		if res.Evaluation != nil {
			continue
		}
		if retryErr != nil {
			res.Error = fmt.Sprintf("%s; recovery failed: %v", res.Error, retryErr)
		} else if eval := recovered[res.Comment.CommentID]; eval != nil {
			res.Evaluation = eval
			res.Error = ""
		}
	}
	return withDenials(results, usage), usage
}

// keepCounters carries the counters prev reported over to usage when usage
// reported none, so a retry without a usage footer does not drop totals the
// CLI already reported. The CLI counters are session-cumulative, so the larger
// value is the later one.
func keepCounters(usage *Usage, prev *Usage) {
	usage.AICredits = max(usage.AICredits, prev.AICredits)
	usage.CostUSD = max(usage.CostUSD, prev.CostUSD)
	usage.InputTokens = max(usage.InputTokens, prev.InputTokens)
	usage.OutputTokens = max(usage.OutputTokens, prev.OutputTokens)
	usage.CachedTokens = max(usage.CachedTokens, prev.CachedTokens)
}

// batchTimeout scales a per-comment timeout to the single invocation that
// covers every comment, so a batch gets as long as the same comments would get
// one at a time.
func batchTimeout(timeout time.Duration, comments int) time.Duration {
	if timeout <= 0 || comments <= 1 {
		return timeout
	}
	return timeout * time.Duration(comments)
}

// withDenials copies usage's Denials onto every result, since they all share
// the single batch invocation that produced usage.
func withDenials(results []*EvaluationResult, usage *Usage) []*EvaluationResult {
	if usage == nil {
		return results
	}
	for _, r := range results {
		r.Denials = usage.Denials
	}
	return results
}

// assignBatchResults pairs each comment with the Evaluation EvaluateBatch
// returned for it, recording an error for comments the Copilot CLI's
// response did not cover.
func assignBatchResults(comments []*Comment, evals map[int64]*Evaluation) []*EvaluationResult {
	results := make([]*EvaluationResult, len(comments))
	for i, c := range comments {
		res := &EvaluationResult{Comment: c}
		if eval, ok := evals[c.CommentID]; ok && eval != nil {
			res.Evaluation = eval
		} else if ok {
			res.Error = fmt.Sprintf("duplicate evaluations returned for comment %d", c.CommentID)
		} else {
			res.Error = fmt.Sprintf("no evaluation returned for comment %d", c.CommentID)
		}
		results[i] = res
	}
	return results
}
