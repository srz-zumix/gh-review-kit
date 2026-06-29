package comments

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewStatsCmd creates the 'comments stats' command.
func NewStatsCmd() *cobra.Command {
	var (
		dataset  string
		groupBy  string
		top      int
		minCount int
		exporter cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Aggregate counts over a comments dataset",
		Long: `Aggregate counts over a comments dataset.

Group records by --group-by (comment_type, repo, author, review_state,
path_prefix, label) and rank by frequency. Use --top to keep only the highest
ranked rows and --min-count to drop noise.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
			}
			result, err := comments.Stats(dataset, comments.StatsOptions{
				GroupBy:  groupBy,
				Top:      top,
				MinCount: minCount,
			})
			if err != nil {
				return fmt.Errorf("failed to compute stats for dataset %q: %w", dataset, err)
			}
			renderer := render.NewRenderer(exporter)
			if err := comments.RenderStats(renderer, result); err != nil {
				return fmt.Errorf("failed to render stats for dataset %q: %w", dataset, err)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&groupBy, "group-by", "comment_type", "Grouping key: comment_type, repo, author, review_state, path_prefix, label")
	f.IntVar(&top, "top", 0, "Keep only the top N rows after sorting (0 = keep all)")
	f.IntVar(&minCount, "min-count", 0, "Drop rows with fewer than this many records")
	cmdutil.AddFormatFlags(cmd, &exporter)
	return cmd
}
