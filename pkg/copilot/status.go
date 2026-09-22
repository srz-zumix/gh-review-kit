package copilot

import (
	"context"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// Status is the state of GitHub Copilot's code review on a pull request.
type Status string

const (
	// StatusNotRequested means Copilot has neither been requested as a
	// reviewer nor submitted a review.
	StatusNotRequested Status = "not_requested"
	// StatusInProgress means Copilot is currently a requested reviewer and
	// has not submitted a review for that request yet.
	StatusInProgress Status = "in_progress"
	// StatusCommented, StatusApproved, StatusChangesRequested and
	// StatusDismissed mirror the state of Copilot's most recent review.
	StatusCommented        Status = "commented"
	StatusApproved         Status = "approved"
	StatusChangesRequested Status = "changes_requested"
	StatusDismissed        Status = "dismissed"
)

// ReviewStatus summarizes GitHub Copilot's code review on a pull request.
type ReviewStatus struct {
	PullRequest        int        `json:"pull_request"`
	Status             Status     `json:"status"`
	Requested          bool       `json:"requested"`
	ReviewCount        int        `json:"review_count"`
	LastReviewState    string     `json:"last_review_state,omitempty"`
	LastReviewedAt     *time.Time `json:"last_reviewed_at,omitempty"`
	Comments           int        `json:"comments"`
	UnresolvedComments int        `json:"unresolved_comments"`
}

// GetReviewStatus reports whether GitHub Copilot is requested as a reviewer on
// a pull request, the state of its most recent review, and how many of its
// review comments are still unresolved.
func GetReviewStatus(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, pull_request any) (*ReviewStatus, error) {
	number, err := gh.GetPullRequestNumber(pull_request)
	if err != nil {
		return nil, err
	}

	reviewers, err := gh.ListPullRequestReviewers(ctx, g, repo, pull_request)
	if err != nil {
		return nil, err
	}
	status := &ReviewStatus{PullRequest: number}
	for _, u := range reviewers.Users {
		if isCopilotLogin(u.GetLogin()) {
			status.Requested = true
			break
		}
	}

	reviews, err := gh.GetPullRequestReviews(ctx, g, repo, pull_request)
	if err != nil {
		return nil, err
	}
	for _, r := range reviews {
		if !isCopilotLogin(r.GetUser().GetLogin()) || r.GetState() == gh.PullRequestReviewStatePending {
			continue
		}
		status.ReviewCount++
		submittedAt := r.GetSubmittedAt().Time
		if status.LastReviewedAt == nil || submittedAt.After(*status.LastReviewedAt) {
			status.LastReviewState = r.GetState()
			status.LastReviewedAt = &submittedAt
		}
	}

	comments, err := ListComments(ctx, g, repo, pull_request, ListOptions{IncludeResolved: true, IncludeOutdated: true})
	if err != nil {
		return nil, err
	}
	status.Comments = len(comments)
	for _, c := range comments {
		if !c.IsResolved {
			status.UnresolvedComments++
		}
	}

	status.Status = deriveStatus(status.Requested, status.LastReviewState)
	return status, nil
}

// deriveStatus maps a pending review request and the most recent review state
// onto a Status. A pending request wins, because it means Copilot is reviewing
// again even when it has already submitted an earlier review.
func deriveStatus(requested bool, lastReviewState string) Status {
	if requested {
		return StatusInProgress
	}
	switch lastReviewState {
	case "":
		return StatusNotRequested
	case gh.PullRequestReviewStateApproved:
		return StatusApproved
	case gh.PullRequestReviewStateChangesRequested:
		return StatusChangesRequested
	case gh.PullRequestReviewStateDismissed:
		return StatusDismissed
	default:
		return StatusCommented
	}
}

// isCopilotLogin reports whether login identifies the Copilot code review bot,
// whose login is returned with or without the "[bot]" suffix depending on the API.
func isCopilotLogin(login string) bool {
	return strings.EqualFold(strings.TrimSuffix(login, "[bot]"), DefaultAuthor)
}

// IsCopilotRequested reports whether GitHub Copilot is currently a requested
// reviewer on a pull request.
func IsCopilotRequested(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, pull_request any) (bool, error) {
	reviewers, err := gh.ListPullRequestReviewers(ctx, g, repo, pull_request)
	if err != nil {
		return false, err
	}
	for _, u := range reviewers.Users {
		if isCopilotLogin(u.GetLogin()) {
			return true, nil
		}
	}
	return false, nil
}
