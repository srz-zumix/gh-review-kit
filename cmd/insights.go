package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/insights"
)

// NewInsightsCmd creates a new parent command for PR review feedback dataset operations.
func NewInsightsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Build and analyze datasets of PR review feedback",
		Long: `Build and analyze datasets of pull request review feedback.

The 'insights' subcommands operate on a dataset directory that stores
normalized JSONL records (corpus.jsonl, prs.jsonl), a manifest, and a
checkpoint. Use 'extract' to populate the dataset from GitHub, then run
'validate' and 'stats' to inspect it.`,
	}

	cmd.AddCommand(insights.NewEstimateCmd())
	cmd.AddCommand(insights.NewExtractCmd())
	cmd.AddCommand(insights.NewValidateCmd())
	cmd.AddCommand(insights.NewStatsCmd())
	cmd.AddCommand(insights.NewSampleCmd())
	cmd.AddCommand(insights.NewBundleCmd())
	cmd.AddCommand(insights.NewSuggestRulesCmd())
	cmd.AddCommand(insights.NewReportCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewInsightsCmd())
}
