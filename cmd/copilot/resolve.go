package copilot

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/internal/commentarg"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// resolutionReasons maps the --reason flag's accepted values to the
// corresponding GraphQL resolution reason.
var resolutionReasons = map[string]gh.PullRequestReviewThreadResolutionReason{
	"addressed": gh.ResolutionReasonAddressed,
	"wont-fix":  gh.ResolutionReasonWontFix,
	"invalid":   gh.ResolutionReasonInvalid,
}

// NewResolveCmd creates a new command to resolve pull request review threads by comment ID.
func NewResolveCmd() *cobra.Command {
	var (
		repo         string
		prIdentifier string
		unresolve    bool
		reason       string
	)

	cmd := &cobra.Command{
		Use:   "resolve <comment-id-or-url>...",
		Short: "Resolve pull request review threads by comment ID",
		Long: `Resolve the review thread containing each of the given pull request review
comment IDs. Use --unresolve to reopen the threads instead.

Each comment can be given as a bare comment ID or as a review comment URL
(https://github.com/owner/repo/pull/123#discussion_r456789); URLs also
determine the target repository and pull request when --repo/--pr are omitted.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if unresolve && reason != "" {
				return fmt.Errorf("--reason cannot be used with --unresolve")
			}
			var resolutionReason gh.PullRequestReviewThreadResolutionReason
			if reason != "" {
				r, ok := resolutionReasons[reason]
				if !ok {
					return fmt.Errorf("invalid --reason %q: must be one of addressed, wont-fix, invalid", reason)
				}
				resolutionReason = r
			}

			opts := []parser.RepositoryOption{parser.RepositoryInput(repo), parser.RepositoryFromURL(prIdentifier)}
			for _, arg := range args {
				opts = append(opts, parser.RepositoryFromURL(arg))
			}
			repository, err := parser.Repository(opts...)
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			ctx := context.Background()

			// Lazily resolved from --pr/current branch, only needed for args without their own PR number.
			var defaultPullRequest any

			for _, arg := range args {
				commentID, prNumber, err := commentarg.Parse(arg)
				if err != nil {
					return err
				}

				pullRequest := any(prNumber)
				if prNumber == 0 {
					if defaultPullRequest == nil {
						pr, err := gh.FindPRByIdentifier(ctx, client, repository, prIdentifier)
						if err != nil {
							return fmt.Errorf("failed to get pull request %s: %w", prIdentifier, err)
						}
						defaultPullRequest = pr
					}
					pullRequest = defaultPullRequest
				}

				action := "Resolved"
				if unresolve {
					action = "Unresolved"
					err = gh.UnresolvePullRequestComment(ctx, client, repository, pullRequest, commentID)
				} else {
					err = gh.ResolvePullRequestComment(ctx, client, repository, pullRequest, commentID, resolutionReason)
				}
				if err != nil {
					return fmt.Errorf("failed to resolve thread for comment %d: %w", commentID, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s thread for comment %d\n", action, commentID)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&prIdentifier, "pr", "", "Pull request number, URL, or branch name (default: current branch)")
	f.StringVar(&reason, "reason", "", "Resolution reason: addressed, wont-fix, or invalid (default: none, cannot be used with --unresolve)")
	f.BoolVar(&unresolve, "unresolve", false, "Reopen the threads instead of resolving them")

	return cmd
}
