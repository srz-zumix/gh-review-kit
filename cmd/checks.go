package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/checks"
)

// NewChecksCmd creates a new parent command for check run operations
func NewChecksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checks",
		Short: "Manage check runs for a pull request",
		Long:  `Manage check runs for a pull request.`,
	}

	cmd.AddCommand(checks.NewListCmd())
	cmd.AddCommand(checks.NewFailureCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewChecksCmd())
}
