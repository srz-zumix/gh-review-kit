package comments

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewExtractCmd creates the 'comments extract' command.
func NewExtractCmd() *cobra.Command {
	var (
		repoFlag     string
		dataset      string
		state        string
		mergedOnly   bool
		sinceFlag    string
		untilFlag    string
		labels       []string
		commentTypes []string
		includeBots  bool
		minLength    int
		paths        []string
		limit        int
		noRedact     bool
		update       bool
	)

	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract PR review feedback into a dataset",
		Long: `Extract pull request review feedback (review bodies, inline review
comments, and PR issue comments) into a normalized JSONL dataset directory.

The dataset directory contains:
  - corpus.jsonl    one JSON record per comment
  - prs.jsonl       one JSON record per PR included in the dataset
  - manifest.json   filter parameters and counts
  - checkpoint.json completed PR numbers, used to resume safely

Re-running the command with the same --dataset resumes from the checkpoint:
PRs already recorded are skipped. Pass --update to additionally re-fetch PRs
whose updated_at advanced since the last run; their existing PR and comment
records are atomically replaced. Conservative secret/token redaction is
applied by default; pass --no-redact to opt out.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dataset == "" {
				return fmt.Errorf("--dataset is required")
			}
			repository, err := parser.Repository(parser.RepositoryInput(repoFlag))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
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

			filters := commentspkg.Filters{
				Repos:        []string{repository.Owner + "/" + repository.Name},
				State:        state,
				MergedOnly:   mergedOnly,
				Since:        since,
				Until:        until,
				Labels:       labels,
				CommentTypes: stringifyTypes(types),
				IncludeBots:  includeBots,
				MinLength:    minLength,
				Paths:        paths,
				Limit:        limit,
				NoRedact:     noRedact,
			}

			ds, err := commentspkg.OpenDataset(dataset, filters)
			if err != nil {
				return fmt.Errorf("failed to open dataset %q: %w", dataset, err)
			}
			defer ds.Close()

			opts := commentspkg.ExtractOptions{
				Repo:         repository,
				State:        state,
				MergedOnly:   mergedOnly,
				Since:        since,
				Until:        until,
				Labels:       labels,
				CommentTypes: types,
				IncludeBots:  includeBots,
				MinLength:    minLength,
				Paths:        paths,
				Limit:        limit,
				NoRedact:     noRedact,
				Update:       update,
			}

			ctx := context.Background()
			if err := commentspkg.Extract(ctx, client, ds, opts); err != nil {
				return fmt.Errorf("failed to extract comments: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote dataset to %s\n", commentspkg.AbsDataset(dataset))
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repoFlag, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&dataset, "dataset", "", "Dataset directory (required)")
	f.StringVar(&state, "state", "all", "PR state filter: open, closed, all")
	f.BoolVar(&mergedOnly, "merged", false, "Include only merged pull requests")
	f.StringVar(&sinceFlag, "since", "", "Only include PRs updated at or after this RFC3339 timestamp (e.g. 2024-01-01T00:00:00Z)")
	f.StringVar(&untilFlag, "until", "", "Only include PRs created at or before this RFC3339 timestamp")
	f.StringSliceVar(&labels, "labels", nil, "Include only PRs that have at least one of the given labels")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Comment types to extract (default: all). Allowed: review_body, review_comment, issue_comment")
	f.BoolVar(&includeBots, "include-bots", false, "Include comments authored by bot users")
	f.IntVar(&minLength, "min-length", 0, "Skip comments whose trimmed body is shorter than this many bytes")
	f.StringSliceVar(&paths, "path", nil, "Restrict inline review comments to these path prefixes (repeatable)")
	f.IntVar(&limit, "limit", 0, "Maximum number of new PRs to process this run (0 = no limit)")
	f.BoolVar(&noRedact, "no-redact", false, "Disable conservative secret/token redaction")
	f.BoolVar(&update, "update", false, "Re-fetch PRs whose updated_at advanced since the last run")

	return cmd
}

func parseTimeFlag(name, value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("failed to parse --%s as RFC3339: %w", name, err)
	}
	return &t, nil
}

func stringifyTypes(types []commentspkg.CommentType) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}
