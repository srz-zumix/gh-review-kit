package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/script"
)

func NewScriptCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "script", Short: "Review shell scripts before executing them"}
	cmd.AddCommand(script.NewReviewCmd(), script.NewRunCmd())
	return cmd
}

func init() {
	rootCmd.AddCommand(NewScriptCmd())
}
