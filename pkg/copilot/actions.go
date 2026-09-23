package copilot

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// negativeReaction is the GitHub reaction content used as negative feedback.
const negativeReaction = "-1"

// Action describes what ApplyEvaluation did, or would do, for a Comment.
type Action string

const (
	ActionNone     Action = "none"
	ActionFeedback Action = "feedback+resolved"
	ActionResolved Action = "resolved"
)

// ApplyEvaluation acts on an Evaluation for comment: when the comment is
// judged invalid, it leaves a thumbs-down reaction and resolves the review
// thread with reason INVALID. When the comment is judged valid, it resolves
// the review thread with reason ADDRESSED. No action is taken for an
// unclear verdict. When dryRun is true, the action that would be taken is
// returned without performing it.
func ApplyEvaluation(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, comment *Comment, eval *Evaluation, dryRun bool) (Action, error) {
	switch eval.Verdict {
	case VerdictInvalid:
		if dryRun {
			return ActionFeedback, nil
		}
		if _, err := gh.AddPullRequestReviewCommentReaction(ctx, g, repo, comment.CommentID, negativeReaction); err != nil {
			return ActionNone, err
		}
		if err := g.ResolveReviewThread(ctx, repo.Owner, repo.Name, comment.ThreadID, gh.ResolutionReasonInvalid); err != nil {
			return ActionNone, fmt.Errorf("failed to resolve thread for comment %d: %w", comment.CommentID, err)
		}
		return ActionFeedback, nil
	case VerdictValid:
		if dryRun {
			return ActionResolved, nil
		}
		if err := g.ResolveReviewThread(ctx, repo.Owner, repo.Name, comment.ThreadID, gh.ResolutionReasonAddressed); err != nil {
			return ActionNone, fmt.Errorf("failed to resolve thread for comment %d: %w", comment.CommentID, err)
		}
		return ActionResolved, nil
	default:
		return ActionNone, nil
	}
}
