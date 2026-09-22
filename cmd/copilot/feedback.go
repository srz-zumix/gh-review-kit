package copilot

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/internal/commentarg"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewFeedbackCmd creates a new command to leave a reaction on pull request review comments.
func NewFeedbackCmd() *cobra.Command {
	var (
		repo         string
		prIdentifier string
		content      string
	)

	cmd := &cobra.Command{
		Use:   "feedback <comment-id-or-url>...",
		Short: "Leave a reaction on pull request review comments",
		Long: `Leave a reaction on one or more pull request review comments, typically to
report that a GitHub Copilot code review comment was incorrect. Defaults to a
thumbs-down (-1) reaction; use --content to send a different reaction.

Each comment can be given as a bare comment ID or as a review comment URL
(https://github.com/owner/repo/pull/123#discussion_r456789); URLs also
determine the target repository when --repo is omitted.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			for _, arg := range args {
				commentID, _, err := commentarg.Parse(arg)
				if err != nil {
					return err
				}
				if _, err := gh.AddPullRequestReviewCommentReaction(ctx, client, repository, commentID, content); err != nil {
					return fmt.Errorf("failed to add reaction to comment %d: %w", commentID, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Added reaction '%s' to comment %d\n", content, commentID)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&prIdentifier, "pr", "", "Pull request number, URL, or branch name (used to resolve the repository when --repo is omitted)")
	f.StringVar(&content, "content", "-1", "Reaction content: +1, -1, laugh, confused, heart, hooray, rocket, or eyes")

	return cmd
}
