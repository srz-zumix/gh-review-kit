package comments

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
)

// NewSuggestRulesCmd creates the 'comments suggest-rules' command.
func NewSuggestRulesCmd() *cobra.Command {
	var (
		dataset      string
		topicsFile   string
		minCount     int
		minReviewers int
		examples     int
		output       string
		format       string
		commentTypes []string
		reviewStates []string
		paths        []string
		sinceFlag    string
		untilFlag    string
		minLength    int
		includeBots  bool
	)

	cmd := &cobra.Command{
		Use:   "suggest-rules",
		Short: "Rank candidate coding rules / review viewpoints",
		Long: `Rank deterministic candidate coding rules and review viewpoints
inferred from the dataset.

Topic detection is regex/keyword based and case-insensitive. Provide your own
dictionary with --topics-file (JSON), or use the built-in defaults that cover
common review areas (naming, error handling, tests, security, performance,
concurrency, style, logging, API design, comments and docs).

Each candidate is reported with frequency, distinct reviewers, distinct
repos, blocking (CHANGES_REQUESTED) share, latest occurrence, and evidence
URLs. No fuzzy clustering or embeddings are used; the output is reproducible.`,
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
			result, err := commentspkg.SuggestRules(dataset, commentspkg.SuggestRulesOptions{
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
			})
			if err != nil {
				return fmt.Errorf("failed to suggest rules: %w", err)
			}
			return writeRulesOutput(cmd, result, output, format)
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&topicsFile, "topics-file", "", "JSON dictionary of topics (default: built-in)")
	f.IntVar(&minCount, "min-count", 3, "Drop topics matched fewer than this many times")
	f.IntVar(&minReviewers, "min-reviewers", 2, "Drop topics matched by fewer than this many distinct reviewers")
	f.IntVar(&examples, "examples", 3, "Number of evidence examples to include per topic")
	f.StringVar(&output, "output", "", "Output file path (default: stdout)")
	f.StringVar(&format, "format", "text", "Output format: text, json, markdown")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Filter: comment types (review_body, review_comment, issue_comment)")
	f.StringSliceVar(&reviewStates, "review-states", nil, "Filter: review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED)")
	f.StringSliceVar(&paths, "path", nil, "Filter: path prefixes for inline review comments")
	f.StringVar(&sinceFlag, "since", "", "Filter: created at or after this RFC3339 timestamp")
	f.StringVar(&untilFlag, "until", "", "Filter: created at or before this RFC3339 timestamp")
	f.IntVar(&minLength, "min-length", 0, "Filter: minimum trimmed body length in bytes")
	f.BoolVar(&includeBots, "include-bots", false, "Include bot-authored comments")
	return cmd
}

func writeRulesOutput(cmd *cobra.Command, r *commentspkg.SuggestRulesResult, output, format string) error {
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
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case "markdown":
		report := &commentspkg.Report{Dataset: r.Dataset, Rules: r, Stats: map[string]*commentspkg.StatsResult{}}
		return commentspkg.WriteReportMarkdown(w, report)
	case "text", "":
		fmt.Fprintf(w, "Dataset: %s\n", r.Dataset)
		fmt.Fprintf(w, "Topics evaluated: %d\n", r.Topics)
		fmt.Fprintf(w, "Candidates: %d\n", len(r.Candidates))
		fmt.Fprintln(w)
		for _, c := range r.Candidates {
			fmt.Fprintf(w, "- %s (count=%d reviewers=%d repos=%d blocking=%d/%.0f%%)\n",
				c.Topic, c.Count, c.DistinctReviewers, c.DistinctRepos, c.BlockingCount, c.BlockingShare*100)
			if c.Description != "" {
				fmt.Fprintf(w, "    %s\n", c.Description)
			}
			for _, ex := range c.Examples {
				fmt.Fprintf(w, "    - %s#%d %s\n", ex.Repo, ex.PRNumber, ex.URL)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown --format %q (allowed: text, json, markdown)", format)
	}
}
