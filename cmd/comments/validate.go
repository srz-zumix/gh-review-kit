package comments

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
)

// NewValidateCmd creates the 'comments validate' command.
func NewValidateCmd() *cobra.Command {
	var (
		dataset string
		strict  bool
		format  string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a comments dataset",
		Long: `Validate the schema and integrity of a comments dataset directory.

Checks include schema version, required fields, duplicate IDs, and PR/comment
linkage. Use --strict to exit non-zero when any issue is reported.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
			}
			report, err := commentspkg.Validate(dataset)
			if err != nil {
				return fmt.Errorf("failed to validate dataset %q: %w", dataset, err)
			}
			if format == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return fmt.Errorf("failed to encode report: %w", err)
				}
			} else {
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "Comments: %d\n", report.Comments)
				fmt.Fprintf(out, "PRs:      %d\n", report.PRs)
				fmt.Fprintf(out, "Duplicate comment IDs: %d\n", report.DuplicateIDs)
				if len(report.Issues) == 0 {
					fmt.Fprintln(out, "No issues found.")
				} else {
					fmt.Fprintf(out, "Issues (%d):\n", len(report.Issues))
					for _, issue := range report.Issues {
						fmt.Fprintf(out, "  - %s\n", issue)
					}
				}
			}
			if strict && len(report.Issues) > 0 {
				return fmt.Errorf("validation found %d issue(s)", len(report.Issues))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.BoolVar(&strict, "strict", false, "Exit non-zero when any issue is reported")
	f.StringVar(&format, "format", "text", "Output format: text, json")
	return cmd
}
