package copilot

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/copilot/settings"
)

// NewSettingsCmd creates a parent command for Copilot code review settings.
func NewSettingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage GitHub Copilot code review settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(settings.NewSetCmd())
	cmd.AddCommand(settings.NewViewCmd())

	return cmd
}
