package comments

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewValidateCmd creates the 'comments validate' command.
func NewValidateCmd() *cobra.Command {
	var (
		dataset  string
		strict   bool
		exporter cmdutil.Exporter
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
			report, err := comments.Validate(dataset)
			if err != nil {
				return fmt.Errorf("failed to validate dataset %q: %w", dataset, err)
			}
			r := render.NewRenderer(exporter)
			if r.HasExporter() {
				if err := r.RenderExportedData(report); err != nil {
					return fmt.Errorf("failed to encode report: %w", err)
				}
			} else {
				r.WriteLine(fmt.Sprintf("Comments: %d", report.Comments))
				r.WriteLine(fmt.Sprintf("PRs:      %d", report.PRs))
				r.WriteLine(fmt.Sprintf("Duplicate comment IDs: %d", report.DuplicateIDs))
				if len(report.Issues) == 0 {
					r.WriteLine("No issues found.")
				} else {
					r.WriteLine(fmt.Sprintf("Issues (%d):", len(report.Issues)))
					for _, issue := range report.Issues {
						r.WriteLine("  - " + issue)
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
	cmdutil.AddFormatFlags(cmd, &exporter)
	return cmd
}
