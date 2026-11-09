package insights

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/insights"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewStatsCmd creates the 'insights stats' command.
func NewStatsCmd() *cobra.Command {
	var (
		dataset  string
		groupBy  string
		top      int
		minCount int
		format   string
		exporter cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Aggregate counts over a comments dataset",
		Long: `Aggregate counts over a comments dataset.

Group records by --group-by (comment_type, repo, author, review_state,
path_prefix, label, copilot_source, copilot_effort) and rank by frequency. Use
--top to keep only the highest ranked rows and --min-count to drop noise.

copilot_source groups Copilot code review comments by the skill, MCP server, or
custom instructions file Copilot reported as their source, and copilot_effort
groups by the review effort. Records without attribution, including datasets
extracted before attribution was recorded, fall into "(none)"; extract into a
new dataset to record it. Copilot comments are bot comments, so extract with
--include-bots to keep them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
			}
			result, err := insights.Stats(dataset, insights.StatsOptions{
				GroupBy:  groupBy,
				Top:      top,
				MinCount: minCount,
				// Copilot code review comments are bot-authored; keep bot
				// records for the Copilot attribution groupings so they are
				// not discarded before grouping.
				Filters: insights.SampleFilters{
					IncludeBots: groupBy == "copilot_source" || groupBy == "copilot_effort",
				},
			})
			if err != nil {
				return fmt.Errorf("failed to compute stats for dataset %q: %w", dataset, err)
			}
			renderer := render.NewRenderer(exporter)
			if err := insights.RenderStats(renderer, result); err != nil {
				return fmt.Errorf("failed to render stats for dataset %q: %w", dataset, err)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&groupBy, "group-by", "comment_type", "Grouping key: comment_type, repo, author, review_state, path_prefix, label, copilot_source, copilot_effort")
	f.IntVar(&top, "top", 0, "Keep only the top N rows after sorting (0 = keep all)")
	f.IntVar(&minCount, "min-count", 0, "Drop rows with fewer than this many records")
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}
	return cmd
}
