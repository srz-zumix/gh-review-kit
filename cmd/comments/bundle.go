package comments

import (
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewBundleCmd creates the 'comments bundle' command.
func NewBundleCmd() *cobra.Command {
	var (
		dataset      string
		outputDir    string
		groupBy      string
		maxRecords   int
		maxBytes     int64
		commentTypes []string
		reviewStates []string
		authors      []string
		paths        []string
		sinceFlag    string
		untilFlag    string
		minLength    int
		includeBots  bool
		exporter     cmdutil.Exporter
	)

	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Split a dataset into Agent-sized JSONL bundles",
		Long: `Split a comments dataset into smaller JSONL bundles for parallel
LLM/Agent analysis.

The corpus is filtered, optionally grouped by --group-by, and written into
--output-dir as bundle-NNNN.jsonl files capped by --max-records and/or
--max-bytes. A manifest.json next to the bundles records each file's group,
record count, and byte size for reproducibility.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
			}
			if outputDir == "" {
				return fmt.Errorf("--output-dir is required")
			}
			since, err := parseTimeFlag("since", sinceFlag)
			if err != nil {
				return err
			}
			until, err := parseTimeFlag("until", untilFlag)
			if err != nil {
				return err
			}
			types, err := comments.CommentTypesFromStrings(commentTypes)
			if err != nil {
				return fmt.Errorf("invalid --comment-types: %w", err)
			}
			opts := comments.BundleOptions{
				OutputDir:  outputDir,
				GroupBy:    groupBy,
				MaxRecords: maxRecords,
				MaxBytes:   maxBytes,
				Filters: comments.SampleFilters{
					CommentTypes: types,
					ReviewStates: normalizeStates(reviewStates),
					Authors:      authors,
					PathPrefixes: paths,
					Since:        since,
					Until:        until,
					MinLength:    minLength,
					IncludeBots:  includeBots,
				},
			}
			manifest, err := comments.Bundle(dataset, opts)
			if err != nil {
				return fmt.Errorf("failed to bundle dataset %q: %w", dataset, err)
			}
			r := render.NewRenderer(exporter)
			if r.HasExporter() {
				return r.RenderExportedData(manifest)
			}
			r.WriteLine(fmt.Sprintf("Wrote %d bundle(s) to %s", len(manifest.Bundles), outputDir))
			for _, b := range manifest.Bundles {
				if b.Group != "" {
					r.WriteLine(fmt.Sprintf("  %s [%s] records=%d bytes=%d", b.File, b.Group, b.Records, b.Bytes))
				} else {
					r.WriteLine(fmt.Sprintf("  %s records=%d bytes=%d", b.File, b.Records, b.Bytes))
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&outputDir, "output-dir", "", "Directory to write bundle files (required)")
	f.StringVar(&groupBy, "group-by", "", "Grouping key: comment_type, repo, author, review_state, path_prefix, label (empty = single stream)")
	f.IntVar(&maxRecords, "max-records", 0, "Maximum records per bundle (0 = no record cap)")
	f.Int64Var(&maxBytes, "max-bytes", 0, "Maximum bytes per bundle (0 = no byte cap)")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Filter: comment types (review_body, review_comment, issue_comment)")
	f.StringSliceVar(&reviewStates, "review-states", nil, "Filter: review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED)")
	f.StringSliceVar(&authors, "authors", nil, "Filter: authors")
	f.StringSliceVar(&paths, "path", nil, "Filter: path prefixes for inline review comments")
	f.StringVar(&sinceFlag, "since", "", "Filter: created at or after this RFC3339 timestamp")
	f.StringVar(&untilFlag, "until", "", "Filter: created at or before this RFC3339 timestamp")
	f.IntVar(&minLength, "min-length", 0, "Filter: minimum trimmed body length in bytes")
	f.BoolVar(&includeBots, "include-bots", false, "Include bot-authored comments")
	cmdutil.AddFormatFlags(cmd, &exporter)
	return cmd
}
