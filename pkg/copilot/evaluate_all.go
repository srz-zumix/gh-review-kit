package copilot

import (
	"context"
	"fmt"
	"slices"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// EvaluateAll judges every comment of a pull request and applies each
// verdict (see ApplyEvaluation), either with one Copilot CLI invocation per
// comment (batch is false) or a single invocation covering every comment
// (batch is true). It returns one EvaluationResult per comment, in the same
// order as comments, plus the most recent Usage the Copilot CLI reported.
func EvaluateAll(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, opts EvaluateOptions, repoSlug string, prNumber int, comments []*Comment, batch bool, dryRun bool) ([]*EvaluationResult, *Usage) {
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
	return results, usage
}

// evaluateAllSequential runs one Copilot CLI invocation per comment.
func evaluateAllSequential(ctx context.Context, opts EvaluateOptions, repoSlug string, prNumber int, comments []*Comment) ([]*EvaluationResult, *Usage) {
	results := make([]*EvaluationResult, 0, len(comments))
	var usage *Usage
	var allDenials []string
	var allRecommendations []string
	var quotaExceeded bool
	for _, c := range comments {
		if opts.Log != nil {
			fmt.Fprintf(opts.Log, "\n--- evaluating comment %d (%s) ---\n", c.CommentID, c.URL)
		}
		res := &EvaluationResult{Comment: c}
		eval, u, err := Evaluate(ctx, opts, repoSlug, prNumber, c)
		if u != nil {
			usage = u
			res.Denials = u.Denials
			allDenials = append(allDenials, u.Denials...)
			allRecommendations = appendUnique(allRecommendations, u.Recommendations)
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
	if opts.Log != nil {
		fmt.Fprintf(opts.Log, "\n--- evaluating %d comments in a single batch ---\n", len(comments))
	}
	evals, usage, err := EvaluateBatch(ctx, opts, repoSlug, prNumber, comments)
	if err != nil {
		results := make([]*EvaluationResult, len(comments))
		for i, c := range comments {
			results[i] = &EvaluationResult{Comment: c, Error: err.Error()}
		}
		return withDenials(results, usage), usage
	}
	return withDenials(assignBatchResults(comments, evals), usage), usage
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
		if eval, ok := evals[c.CommentID]; ok {
			res.Evaluation = eval
		} else {
			res.Error = fmt.Sprintf("no evaluation returned for comment %d", c.CommentID)
		}
		results[i] = res
	}
	return results
}
