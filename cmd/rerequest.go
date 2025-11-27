package cmd

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewRerequestCmd creates a new command to re-request review for a pull request
func NewRerequestCmd() *cobra.Command {
	var (
		repo            string
		reviewers       []string
		excludeApproved bool
		expandTeam      bool
	)

	cmd := &cobra.Command{
		Use:   "rerequest <pull-request-number>",
		Short: "Re-request review for a pull request",
		Long: `Re-request review for a pull request.

If reviewers are not specified, the command will re-request review from all reviewers who have already submitted a review.
If reviewers are specified, the command will re-request review from the specified reviewers only.

Reviewers can be specified as:
  - Individual users: username
  - Team reviewers: org/team-slug
  - With @ prefix: @username or @org/team-slug

When --expand-team is specified, team reviewers will be expanded to individual team members.`,
		Aliases: []string{"rr"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prNumber := args[0]

			repository, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			ctx := context.Background()

			// Get pull request
			pr, err := gh.GetPullRequest(ctx, client, repository, prNumber)
			if err != nil {
				return fmt.Errorf("failed to get pull request #%s: %w", prNumber, err)
			}

			var reviewersRequest gh.ReviewersRequest

			if len(reviewers) > 0 {
				// Use specified reviewers
				reviewersRequest = gh.GetRequestedReviewers(reviewers)

				// If expandTeam is set, expand team reviewers to individual members
				if expandTeam {
					expanded, err := gh.ExpandTeamReviewers(ctx, client, repository, reviewersRequest)
					if err != nil {
						return fmt.Errorf("failed to expand team reviewers: %w", err)
					}
					reviewersRequest = expanded
					logger.Info("Expanded team reviewers to individual members", "count", len(reviewersRequest.Reviewers))
				}

				// If excludeApproved is set, filter out approved reviewers
				if excludeApproved {
					approvedReviewers, err := gh.GetApprovedReviewers(ctx, client, repository, prNumber)
					if err != nil {
						return fmt.Errorf("failed to get approved reviewers for pull request #%s: %w", prNumber, err)
					}

					// Create a map for fast lookup
					approvedMap := make(map[string]bool)
					for _, reviewer := range approvedReviewers {
						approvedMap[reviewer] = true
					}

					// Filter out approved reviewers
					filteredReviewers := []string{}
					for _, reviewer := range reviewersRequest.Reviewers {
						if approvedMap[reviewer] {
							logger.Info("Skipping approved reviewer", "reviewer", reviewer)
							continue
						}
						filteredReviewers = append(filteredReviewers, reviewer)
					}
					reviewersRequest.Reviewers = filteredReviewers

					// Filter out approved team reviewers
					filteredTeamReviewers := []string{}
					for _, team := range reviewersRequest.TeamReviewers {
						members, err := gh.ListTeamMembers(ctx, client, repository, team, nil, false)
						if err != nil {
							return fmt.Errorf("failed to get members of team '%s': %w", team, err)
						}
						memberNames := gh.GetUserNames(members)
						if slices.ContainsFunc(memberNames, func(s string) bool {
							return approvedMap[s]
						}) {
							logger.Info("Skipping approved team reviewer member", "team", team, "member", memberNames)
							continue
						}
						filteredTeamReviewers = append(filteredTeamReviewers, team)
					}
					reviewersRequest.TeamReviewers = filteredTeamReviewers

					if len(reviewersRequest.Reviewers) == 0 && len(reviewersRequest.TeamReviewers) == 0 {
						return fmt.Errorf("no eligible reviewers found for pull request #%d (all specified reviewers have already approved)", pr.GetNumber())
					}
				}

				logger.Info("Re-requesting review from specified reviewers", "pr", pr.GetNumber())
			} else {
				// Get reviewers who have already submitted reviews
				reviews, err := gh.GetPullRequestLatestReviews(ctx, client, repository, prNumber)
				if err != nil {
					return fmt.Errorf("failed to get reviews for pull request #%s: %w", prNumber, err)
				}

				if len(reviews) == 0 {
					return fmt.Errorf("no reviews found for pull request #%d, please specify reviewers using --reviewers flag", pr.GetNumber())
				}

				// Build reviewers list from reviewers who have submitted reviews
				for _, review := range reviews {
					// Skip approved reviews if excludeApproved is set
					if excludeApproved && review.GetState() == gh.PullRequestReviewStateApproved {
						logger.Info("Skipping approved reviewer", "reviewer", review.User.GetLogin())
						continue
					}
					reviewersRequest.Reviewers = append(reviewersRequest.Reviewers, review.User.GetLogin())
				}

				if len(reviewersRequest.Reviewers) == 0 {
					return fmt.Errorf("no eligible reviewers found for pull request #%d (all reviewers may be approved)", pr.GetNumber())
				}

				logger.Info("Re-requesting review from reviewers who have already reviewed", "pr", pr.GetNumber())
			}

			// Request reviewers
			_, err = gh.RequestPullRequestReviewers(ctx, client, repository, prNumber, reviewersRequest)
			if err != nil {
				return fmt.Errorf("failed to re-request review for pull request #%s: %w", prNumber, err)
			}

			fmt.Fprintf(os.Stdout, "Successfully re-requested review for pull request #%d\n", pr.GetNumber())
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringSliceVarP(&reviewers, "reviewers", "r", []string{}, "Reviewers to re-request (users or teams, e.g., username or org/team)")
	f.BoolVar(&excludeApproved, "exclude-approved", false, "Exclude reviewers who have already approved")
	f.BoolVar(&expandTeam, "expand-team", false, "Expand team reviewers to individual team members")

	return cmd
}

func init() {
	rootCmd.AddCommand(NewRerequestCmd())
}
