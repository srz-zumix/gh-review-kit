package comments

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
)

// NewStatsCmd creates the 'comments stats' command.
func NewStatsCmd() *cobra.Command {
	var (
		dataset  string
		groupBy  string
		top      int
		minCount int
		format   string
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
			result, err := commentspkg.Stats(dataset, commentspkg.StatsOptions{
				GroupBy:  groupBy,
				Top:      top,
				MinCount: minCount,
			})
			if err != nil {
				return fmt.Errorf("failed to compute stats for dataset %q: %w", dataset, err)
			}
			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(result); err != nil {
					return fmt.Errorf("failed to encode stats: %w", err)
				}
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "KEY\tCOUNT\tREVIEWERS\tREPOS\tCHANGES_REQUESTED\tEXAMPLE")
			for _, row := range result.Rows {
				fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n", row.Key, row.Count, row.Reviewers, row.Repos, row.Blocking, row.ExampleURL)
			}
			return tw.Flush()
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&groupBy, "group-by", "comment_type", "Grouping key: comment_type, repo, author, review_state, path_prefix, label")
	f.IntVar(&top, "top", 0, "Keep only the top N rows after sorting (0 = keep all)")
	f.IntVar(&minCount, "min-count", 0, "Drop rows with fewer than this many records")
	f.StringVar(&format, "format", "text", "Output format: text, json")
	return cmd
}
