package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/cmd/comments"
)

// NewCommentsCmd creates a new parent command for PR comment dataset operations.
func NewCommentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comments",
		Short: "Build and analyze datasets of PR review feedback",
		Long: `Build and analyze datasets of pull request review feedback.

The 'comments' subcommands operate on a dataset directory that stores
normalized JSONL records (corpus.jsonl, prs.jsonl), a manifest, and a
checkpoint. Use 'extract' to populate the dataset from GitHub, then run
'validate' and 'stats' to inspect it.`,
	}

	cmd.AddCommand(comments.NewEstimateCmd())
	cmd.AddCommand(comments.NewExtractCmd())
	cmd.AddCommand(comments.NewValidateCmd())
	cmd.AddCommand(comments.NewStatsCmd())
	cmd.AddCommand(comments.NewSampleCmd())
	cmd.AddCommand(comments.NewBundleCmd())
	cmd.AddCommand(comments.NewSuggestRulesCmd())
	cmd.AddCommand(comments.NewReportCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewCommentsCmd())
}
