package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/copilot"
)

// NewCopilotCmd creates a new parent command for working with Copilot code review comments.
func NewCopilotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copilot",
		Short: "Review and act on GitHub Copilot code review comments",
		Long: `Fetch GitHub Copilot code review comments on a pull request, optionally judge
them with the Copilot CLI, and act on the verdict: leave negative feedback and
resolve the thread for comments judged incorrect.`,
	}

	cmd.AddCommand(copilot.NewAssignCmd())
	cmd.AddCommand(copilot.NewCommentsCmd())
	cmd.AddCommand(copilot.NewFeedbackCmd())
	cmd.AddCommand(copilot.NewResolveCmd())
	cmd.AddCommand(copilot.NewStatusCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewCopilotCmd())
}
