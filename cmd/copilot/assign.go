package copilot

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/copilot"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewAssignCmd creates a new command to assign GitHub Copilot as a reviewer on a pull request.
func NewAssignCmd() *cobra.Command {
	var (
		repo  string
		force bool
	)

	cmd := &cobra.Command{
		Use:   "assign [pull-request-number]",
		Short: "Assign GitHub Copilot as a reviewer on a pull request",
		Long: `Request a code review from GitHub Copilot on a pull request, the same as
requesting a review from a human reviewer.

If pull-request-number is omitted, the pull request for the current branch is used.

By default, it is an error to assign Copilot when it is already a requested
reviewer or has already reviewed the pull request. Use --force to request a
review again anyway.`,
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
					return fmt.Errorf("failed to check whether Copilot is already assigned as a reviewer for pull request #%d: %w", pr.GetNumber(), err)
				}
				if status.Status != copilot.StatusNotRequested {
					return fmt.Errorf("pull request #%d is already assigned to Copilot as a reviewer, use --force to assign again", pr.GetNumber())
				}
			}

			reviewersRequest := gh.ReviewersRequest{Reviewers: []string{copilot.ReviewerLogin}}
			if _, err := gh.RequestPullRequestReviewers(ctx, client, repository, pr, reviewersRequest); err != nil {
				return fmt.Errorf("failed to assign Copilot as a reviewer for pull request #%d: %w", pr.GetNumber(), err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Assigned Copilot as a reviewer for pull request #%d\n", pr.GetNumber())

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.BoolVar(&force, "force", false, "Assign Copilot even if it is already a requested reviewer")

	return cmd
}
