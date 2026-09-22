package copilot

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/copilot"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewReviewRequestCmd creates a new command to request a GitHub Copilot code review on a pull request.
func NewReviewRequestCmd() *cobra.Command {
	var (
		repo  string
		force bool
	)

	cmd := &cobra.Command{
		Use:     "review-request [pull-request-number]",
		Aliases: []string{"rr"},
		Short:   "Request a GitHub Copilot code review on a pull request",
		Long: `Request a code review from GitHub Copilot on a pull request, the same as
requesting a review from a human reviewer.

The request is only sent when the latest commit has not been reviewed yet:

  Copilot has never been requested            request a review
  Copilot reviewed an earlier commit only     request a review again
  Copilot is a requested reviewer already     nothing to do
  Copilot has reviewed the latest commit      nothing to do

Use --force to request a review regardless of that state.

If pull-request-number is omitted, the pull request for the current branch is used.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prIdentifier := ""
			if len(args) > 0 {
				prIdentifier = args[0]
			}

			repository, err := parser.Repository(parser.RepositoryInput(repo), parser.RepositoryFromURL(prIdentifier))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			ctx := context.Background()

			pr, err := gh.FindPRByIdentifier(ctx, client, repository, prIdentifier)
			if err != nil {
				return fmt.Errorf("failed to get pull request %s: %w", prIdentifier, err)
			}

			if !force {
				status, err := copilot.GetReviewStatus(ctx, client, repository, pr)
				if err != nil {
					return fmt.Errorf("failed to check the Copilot review status of pull request #%d: %w", pr.GetNumber(), err)
				}
				request, reason := copilot.DecideReviewRequest(status)
				if !request {
					fmt.Fprintf(cmd.OutOrStdout(), "Skipped pull request #%d: %s\n", pr.GetNumber(), reason)
					return nil
				}
				logger.Info("Requesting a Copilot review", "pr", pr.GetNumber(), "reason", reason)
			}

			reviewersRequest := gh.ReviewersRequest{Reviewers: []string{copilot.ReviewerLogin}}
			if _, err := gh.RequestPullRequestReviewers(ctx, client, repository, pr, reviewersRequest); err != nil {
				return fmt.Errorf("failed to request a Copilot review on pull request #%d: %w", pr.GetNumber(), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Requested a Copilot review on pull request #%d\n", pr.GetNumber())

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.BoolVar(&force, "force", false, "Request a review even when the latest commit has already been reviewed")

	return cmd
}
