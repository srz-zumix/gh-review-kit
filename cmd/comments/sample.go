package comments

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
)

// NewSampleCmd creates the 'comments sample' command.
func NewSampleCmd() *cobra.Command {
	var (
		dataset      string
		groupBy      string
		perGroup     int
		strategy     string
		seed         int64
		output       string
		format       string
		exporter     cmdutil.Exporter
		commentTypes []string
		reviewStates []string
		authors      []string
		paths        []string
		sinceFlag    string
		untilFlag    string
		minLength    int
		includeBots  bool
	)

	cmd := &cobra.Command{
		Use:   "sample",
		Short: "Pick representative comments from a dataset",
		Long: `Pick representative comments from a comments dataset.

Filters narrow the corpus, then records are grouped by --group-by and the
selection --strategy decides which N records (--per-group) are kept per
group. Useful for handing a small evidence set to an LLM/Agent.

Strategies:
  recent          newest first by created_at (default)
  diverse-authors newest record per distinct author until N
  blocking        only review_state=CHANGES_REQUESTED, then recent
  random          random with --seed (deterministic when seeded)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
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
			strat, err := comments.ParseSampleStrategy(strategy)
			if err != nil {
				return err
			}

			opts := comments.SampleOptions{
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
				GroupBy:  groupBy,
				PerGroup: perGroup,
				Strategy: strat,
				Seed:     seed,
			}

			records, err := comments.Sample(dataset, opts)
			if err != nil {
				return fmt.Errorf("failed to sample dataset %q: %w", dataset, err)
			}
			return writeSampleOutput(cmd, records, output, format, exporter)
		},
	}

	f := cmd.Flags()
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&groupBy, "group-by", "", "Grouping key: comment_type, repo, author, review_state, path_prefix, label (empty = single group)")
	f.IntVar(&perGroup, "per-group", 5, "Records to keep per group")
	f.StringVar(&strategy, "strategy", "recent", "Selection strategy: recent, diverse-authors, blocking, random")
	f.Int64Var(&seed, "seed", 0, "Random seed when --strategy=random (0 = time-based)")
	f.StringVar(&output, "output", "", "Output file path (default: stdout)")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Filter: comment types (review_body, review_comment, issue_comment)")
	f.StringSliceVar(&reviewStates, "review-states", nil, "Filter: review states (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED)")
	f.StringSliceVar(&authors, "authors", nil, "Filter: authors")
	f.StringSliceVar(&paths, "path", nil, "Filter: path prefixes for inline review comments")
	f.StringVar(&sinceFlag, "since", "", "Filter: created at or after this RFC3339 timestamp")
	f.StringVar(&untilFlag, "until", "", "Filter: created at or before this RFC3339 timestamp")
	f.IntVar(&minLength, "min-length", 0, "Filter: minimum trimmed body length in bytes")
	f.BoolVar(&includeBots, "include-bots", false, "Include bot-authored comments")
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "jsonl", []string{"jsonl"}); err != nil {
		panic(err)
	}
	return cmd
}

func normalizeStates(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(strings.ToUpper(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func writeSampleOutput(cmd *cobra.Command, records []*comments.Comment, output, format string, exporter cmdutil.Exporter) error {
	var w = cmd.OutOrStdout()
	if output != "" {
		f, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("failed to open output file: %w", err)
		}
		defer f.Close()
		w = f
	}
	if exporter != nil {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	enc := json.NewEncoder(w)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
