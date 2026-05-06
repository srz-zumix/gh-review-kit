package comments

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
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
		format       string
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
			types, err := commentspkg.CommentTypesFromStrings(commentTypes)
			if err != nil {
				return fmt.Errorf("invalid --comment-types: %w", err)
			}
			opts := commentspkg.BundleOptions{
				OutputDir:  outputDir,
				GroupBy:    groupBy,
				MaxRecords: maxRecords,
				MaxBytes:   maxBytes,
				Filters: commentspkg.SampleFilters{
					CommentTypes: types,
					ReviewStates: bundleNormalizeStates(reviewStates),
					Authors:      authors,
					PathPrefixes: paths,
					Since:        since,
					Until:        until,
					MinLength:    minLength,
					IncludeBots:  includeBots,
				},
			}
			manifest, err := commentspkg.Bundle(dataset, opts)
			if err != nil {
				return fmt.Errorf("failed to bundle dataset %q: %w", dataset, err)
			}
			out := cmd.OutOrStdout()
			if format == "json" {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(manifest)
			}
			fmt.Fprintf(out, "Wrote %d bundle(s) to %s\n", len(manifest.Bundles), outputDir)
			for _, b := range manifest.Bundles {
				if b.Group != "" {
					fmt.Fprintf(out, "  %s [%s] records=%d bytes=%d\n", b.File, b.Group, b.Records, b.Bytes)
				} else {
					fmt.Fprintf(out, "  %s records=%d bytes=%d\n", b.File, b.Records, b.Bytes)
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
	f.StringVar(&format, "format", "text", "Output format for the summary: text, json")
	return cmd
}

func bundleNormalizeStates(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(strings.ToUpper(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
