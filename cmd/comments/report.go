package comments

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
)

// NewReportCmd creates the 'comments report' command.
func NewReportCmd() *cobra.Command {
	var (
		dataset      string
		topicsFile   string
		output       string
		format       string
		minCount     int
		minReviewers int
		examples     int
		statsTop     int
		commentTypes []string
		reviewStates []string
		paths        []string
		sinceFlag    string
		untilFlag    string
		minLength    int
		includeBots  bool
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate a Markdown/JSON report from a comments dataset",
		Long: `Generate a deterministic Markdown or JSON report from a comments
dataset. The report combines aggregate stats (by comment_type, review_state,
author, path_prefix, repo) with rule candidates from 'suggest-rules' and a
manifest summary, so humans can review review-comment trends and sign off on
which topics should become coding rules.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
			}
			var topics *commentspkg.TopicSet
			if topicsFile != "" {
				ts, err := commentspkg.LoadTopicSet(topicsFile)
				if err != nil {
					return err
				}
				topics = ts
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
			r, err := commentspkg.BuildReport(dataset, commentspkg.ReportOptions{
				Topics: topics,
				Filters: commentspkg.SampleFilters{
					CommentTypes: types,
					ReviewStates: normalizeStates(reviewStates),
					PathPrefixes: paths,
					Since:        since,
					Until:        until,
					MinLength:    minLength,
					IncludeBots:  includeBots,
				},
				MinCount:     minCount,
				MinReviewers: minReviewers,
				Examples:     examples,
				StatsTop:     statsTop,
			})
			if err != nil {
				return fmt.Errorf("failed to build report: %w", err)
			}

			w := cmd.OutOrStdout()
			if output != "" {
				f, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
				if err != nil {
					return fmt.Errorf("failed to open output file: %w", err)
				}
				defer f.Close()
				w = f
			}
			switch format {
			case "json":
				return commentspkg.WriteReportJSON(w, r)
			case "markdown", "":
				return commentspkg.WriteReportMarkdown(w, r)
			default:
				return fmt.Errorf("unknown --format %q (allowed: markdown, json)", format)
			}
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&topicsFile, "topics-file", "", "JSON dictionary of topics (default: built-in)")
	f.StringVar(&output, "output", "", "Output file path (default: stdout)")
	f.StringVar(&format, "format", "markdown", "Output format: markdown, json")
	f.IntVar(&minCount, "min-count", 3, "Drop topics matched fewer than this many times")
	f.IntVar(&minReviewers, "min-reviewers", 2, "Drop topics matched by fewer than this many distinct reviewers")
	f.IntVar(&examples, "examples", 3, "Number of evidence examples to include per topic")
	f.IntVar(&statsTop, "stats-top", 20, "Top N rows per stats slice")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Filter: comment types (review_body, review_comment, issue_comment)")
	f.StringSliceVar(&reviewStates, "review-states", nil, "Filter: review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED)")
	f.StringSliceVar(&paths, "path", nil, "Filter: path prefixes for inline review comments")
	f.StringVar(&sinceFlag, "since", "", "Filter: created at or after this RFC3339 timestamp")
	f.StringVar(&untilFlag, "until", "", "Filter: created at or before this RFC3339 timestamp")
	f.IntVar(&minLength, "min-length", 0, "Filter: minimum trimmed body length in bytes")
	f.BoolVar(&includeBots, "include-bots", false, "Include bot-authored comments")
	return cmd
}
