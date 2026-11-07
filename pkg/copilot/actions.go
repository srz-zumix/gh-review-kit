package copilot

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v93/github"
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

// ValidateReplyReasonOption checks that posting a reason has an evaluation.
func ValidateReplyReasonOption(evaluate, replyReason bool) error {
	if replyReason && !evaluate {
		return fmt.Errorf("--reply-reason requires --evaluate")
	}
	return nil
}

func replyEvaluationReason(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, prNumber int, comment *Comment, reason string, dryRun bool) (*EvaluationReply, error) {
	reply := &EvaluationReply{Status: ReplyFailed}
	if strings.TrimSpace(reason) == "" {
		return reply, fmt.Errorf("cannot reply to comment %d: evaluation reason is empty", comment.CommentID)
	}
	if comment.ThreadID == "" {
		return reply, fmt.Errorf("cannot reply to comment %d: review thread ID is missing", comment.CommentID)
	}
	if comment.RootCommentID <= 0 {
		return reply, fmt.Errorf("cannot reply to comment %d: review thread root comment ID is missing", comment.CommentID)
	}
	if dryRun {
		reply.Status = ReplyPlanned
		return reply, nil
	}

	created, err := gh.CreatePullRequestComment(ctx, g, repo, prNumber, &github.CreatePullRequestCommentRequest{
		Body:      reason,
		InReplyTo: &comment.RootCommentID,
	})
	if err != nil {
		return reply, fmt.Errorf("failed to reply to review comment %d: %w", comment.CommentID, err)
	}
	reply.Status = ReplyPosted
	if created == nil {
		return reply, fmt.Errorf("reply to comment %d was created but GitHub returned no comment details", comment.CommentID)
	}
	reply.CommentID = created.GetID()
	reply.URL = created.GetHTMLURL()
	return reply, nil
}

// ApplyEvaluation acts on an Evaluation for comment: when the comment is
// judged invalid, it leaves a thumbs-down reaction and resolves the review
// thread with reason INVALID. When the comment is judged valid, it resolves
// the review thread with reason ADDRESSED. When the verdict is unclear, it
// resolves the review thread with reason WONT_FIX without a reaction. When
// dryRun is true, the action that would be taken is returned without
// performing it.
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
	case VerdictUnclear:
		if dryRun {
			return ActionResolved, nil
		}
		if err := g.ResolveReviewThread(ctx, repo.Owner, repo.Name, comment.ThreadID, gh.ResolutionReasonWontFix); err != nil {
			return ActionNone, fmt.Errorf("failed to resolve thread for comment %d: %w", comment.CommentID, err)
		}
		return ActionResolved, nil
	default:
		return ActionNone, nil
	}
}
