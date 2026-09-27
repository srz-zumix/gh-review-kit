package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/comments"
)

// NewCommentsCmd creates a new parent command for pull request review feedback.
func NewCommentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comments",
		Short: "List review feedback on a pull request",
		Long: `List review feedback on a pull request.

'gh pr view --comments' only returns issue comments, so it misses feedback
left as review bodies or inline review comments. The 'comments' subcommands
fill that gap.`,
	}

	cmd.AddCommand(comments.NewListCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewCommentsCmd())
}
