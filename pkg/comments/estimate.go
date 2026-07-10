package comments

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v88/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// EstimateOptions configures Estimate.
type EstimateOptions struct {
	Repo         repository.Repository
	State        string
	MergedOnly   bool
	Since        *time.Time
	Until        *time.Time
	Labels       []string
	CommentTypes []CommentType
	Limit        int // cap on PR count to consider; 0 = no cap
	SampleSize   int // number of PRs to sample for averages; 0 = default 5
}

// EstimateResult is a deterministic preflight estimate for a future Extract.
type EstimateResult struct {
	Repo                string    `json:"repo"`
	PRCount             int       `json:"pr_count"`
	SampledPRs          int       `json:"sampled_prs"`
	AvgReviewBodies     float64   `json:"avg_review_bodies"`
	AvgReviewComments   float64   `json:"avg_review_comments"`
	AvgIssueComments    float64   `json:"avg_issue_comments"`
	EstReviewBodies     int       `json:"est_review_bodies"`
	EstReviewComments   int       `json:"est_review_comments"`
	EstIssueComments    int       `json:"est_issue_comments"`
	EstTotalComments    int       `json:"est_total_comments"`
	EstAPICalls         int       `json:"est_api_calls"`
	RateLimitRemaining  int       `json:"rate_limit_remaining"`
	RateLimitLimit      int       `json:"rate_limit_limit"`
	RateLimitResetAt    time.Time `json:"rate_limit_reset_at"`
	Warnings            []string  `json:"warnings,omitempty"`
}

// Estimate previews how much GitHub API work an Extract with the same filters
// would consume. PRs are listed in full (cheap REST pagination), then a small
// sample is fetched to compute average comment counts per PR.
func Estimate(ctx context.Context, client *gh.GitHubClient, opts EstimateOptions) (*EstimateResult, error) {
	state := opts.State
	if state == "" {
		state = "all"
	}
	listOpts := []gh.ListPullRequestsOption{
		&gh.ListPullRequestsOptionState{State: state},
		gh.ListPullRequestsOptionSortUpdated(),
		gh.ListPullRequestsOptionDirectionDescending(),
	}
	prs, err := gh.ListPullRequests(ctx, client, opts.Repo, listOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to list pull requests: %w", err)
	}

	matchOpts := ExtractOptions{
		MergedOnly: opts.MergedOnly,
		Since:      opts.Since,
		Until:      opts.Until,
		Labels:     opts.Labels,
	}
	matched := make([]*github.PullRequest, 0, len(prs))
	for _, pr := range prs {
		if !prMatches(pr, matchOpts) {
			continue
		}
		matched = append(matched, pr)
		if opts.Limit > 0 && len(matched) >= opts.Limit {
			break
		}
	}

	sampleSize := opts.SampleSize
	if sampleSize <= 0 {
		sampleSize = 5
	}
	if sampleSize > len(matched) {
		sampleSize = len(matched)
	}

	commentTypes := commentTypeSet(opts.CommentTypes)

	var totalRB, totalRC, totalIC int
	var sampleAPICalls int
	for i := 0; i < sampleSize; i++ {
		pr := matched[i]
		if commentTypes[CommentTypeReviewBody] {
			rvs, err := gh.GetPullRequestReviews(ctx, client, opts.Repo, pr)
			if err != nil {
				return nil, fmt.Errorf("failed to get reviews for PR #%d: %w", pr.GetNumber(), err)
			}
			// Match extract, which skips reviews with no narrative body
			// (e.g. approve-only reviews), so the estimate is not inflated.
			for _, rv := range rvs {
				if strings.TrimSpace(rv.GetBody()) != "" {
					totalRB++
				}
			}
			sampleAPICalls += pages(len(rvs))
		}
		if commentTypes[CommentTypeReviewComment] {
			cs, err := gh.ListPullRequestReviewComments(ctx, client, opts.Repo, pr)
			if err != nil {
				return nil, fmt.Errorf("failed to list review comments for PR #%d: %w", pr.GetNumber(), err)
			}
			totalRC += len(cs)
			sampleAPICalls += pages(len(cs))
		}
		if commentTypes[CommentTypeIssueComment] {
			ics, err := gh.ListIssueComments(ctx, client, opts.Repo, pr)
			if err != nil {
				return nil, fmt.Errorf("failed to list issue comments for PR #%d: %w", pr.GetNumber(), err)
			}
			totalIC += len(ics)
			sampleAPICalls += pages(len(ics))
		}
	}

	prCount := len(matched)
	res := &EstimateResult{
		Repo:       opts.Repo.Owner + "/" + opts.Repo.Name,
		PRCount:    prCount,
		SampledPRs: sampleSize,
	}
	if sampleSize > 0 {
		res.AvgReviewBodies = float64(totalRB) / float64(sampleSize)
		res.AvgReviewComments = float64(totalRC) / float64(sampleSize)
		res.AvgIssueComments = float64(totalIC) / float64(sampleSize)
	}
	res.EstReviewBodies = int(res.AvgReviewBodies*float64(prCount) + 0.5)
	res.EstReviewComments = int(res.AvgReviewComments*float64(prCount) + 0.5)
	res.EstIssueComments = int(res.AvgIssueComments*float64(prCount) + 0.5)
	res.EstTotalComments = res.EstReviewBodies + res.EstReviewComments + res.EstIssueComments

	// API call estimate: PR listing pages + per-PR fetches scaled by sample
	// average pages per PR. Each enabled comment type costs at least 1 page.
	listPages := pages(len(prs))
	avgPerPR := 0.0
	if sampleSize > 0 {
		avgPerPR = float64(sampleAPICalls) / float64(sampleSize)
	}
	res.EstAPICalls = listPages + int(avgPerPR*float64(prCount)+0.5)

	// Best-effort rate limit lookup; failure is non-fatal.
	if rl, _, err := client.GetClient().RateLimit.Get(ctx); err == nil && rl != nil && rl.Core != nil {
		res.RateLimitLimit = rl.Core.Limit
		res.RateLimitRemaining = rl.Core.Remaining
		res.RateLimitResetAt = rl.Core.Reset.Time
	}

	if res.RateLimitRemaining > 0 && res.EstAPICalls > res.RateLimitRemaining {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("estimated %d API calls exceeds remaining rate limit of %d (resets %s)",
				res.EstAPICalls, res.RateLimitRemaining, res.RateLimitResetAt.Format(time.RFC3339)))
	}
	if res.RateLimitRemaining > 0 && res.EstAPICalls > res.RateLimitRemaining*4/5 && res.EstAPICalls <= res.RateLimitRemaining {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("estimated %d API calls is within 80%% of remaining rate limit %d", res.EstAPICalls, res.RateLimitRemaining))
	}
	if prCount >= 1000 {
		res.Warnings = append(res.Warnings,
			"large PR count: consider --since/--until or --limit to chunk the run and avoid secondary rate limits")
	}
	if sampleSize == 0 && prCount > 0 {
		res.Warnings = append(res.Warnings,
			"sample size is 0; comment estimates are unavailable")
	}
	return res, nil
}

// pages returns the number of paginated REST calls implied by n records at the
// default page size of 100. At least one call is needed to discover an empty
// list.
func pages(n int) int {
	if n <= 0 {
		return 1
	}
	return (n + 99) / 100
}
