package comments

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
	commentspkg "github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewEstimateCmd creates the 'comments estimate' command.
func NewEstimateCmd() *cobra.Command {
	var (
		repoFlag     string
		state        string
		mergedOnly   bool
		sinceFlag    string
		untilFlag    string
		labels       []string
		commentTypes []string
		limit        int
		sampleSize   int
		format       string
	)

	cmd := &cobra.Command{
		Use:   "estimate",
		Short: "Preflight a comments extract: PR count, comment volume, API budget",
		Long: `Estimate how much GitHub API work a future 'comments extract' run with the
same filters would consume.

The command lists matching pull requests (cheap REST pagination), samples a
small number of them to measure average comment volume per PR, and reports the
projected total comments, projected API calls, and current rate-limit headroom.
Use it before kicking off a large extraction to avoid hitting secondary rate
limits or running out of REST quota mid-run.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			result, err := commentspkg.Estimate(context.Background(), client, commentspkg.EstimateOptions{
				Repo:         repository,
				State:        state,
				MergedOnly:   mergedOnly,
				Since:        since,
				Until:        until,
				Labels:       labels,
				CommentTypes: types,
				Limit:        limit,
				SampleSize:   sampleSize,
			})
			if err != nil {
				return fmt.Errorf("failed to estimate: %w", err)
			}
			return writeEstimateOutput(cmd, result, format)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repoFlag, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&state, "state", "all", "PR state filter: open, closed, all")
	f.BoolVar(&mergedOnly, "merged", false, "Include only merged pull requests")
	f.StringVar(&sinceFlag, "since", "", "Only include PRs updated at or after this RFC3339 timestamp")
	f.StringVar(&untilFlag, "until", "", "Only include PRs created at or before this RFC3339 timestamp")
	f.StringSliceVar(&labels, "labels", nil, "Include only PRs that have at least one of the given labels")
	f.StringSliceVar(&commentTypes, "comment-types", nil, "Comment types to estimate (default: all). Allowed: review_body, review_comment, issue_comment")
	f.IntVar(&limit, "limit", 0, "Cap PR count to consider (0 = no cap)")
	f.IntVar(&sampleSize, "sample-size", 5, "Number of PRs to sample for averages")
	f.StringVar(&format, "format", "text", "Output format: text, json")
	return cmd
}

func writeEstimateOutput(cmd *cobra.Command, r *commentspkg.EstimateResult, format string) error {
	w := cmd.OutOrStdout()
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case "text", "":
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Repo:\t%s\n", r.Repo)
		fmt.Fprintf(tw, "Matched PRs:\t%d\n", r.PRCount)
		fmt.Fprintf(tw, "Sampled PRs:\t%d\n", r.SampledPRs)
		fmt.Fprintf(tw, "Avg per PR:\treview_bodies=%.2f review_comments=%.2f issue_comments=%.2f\n",
			r.AvgReviewBodies, r.AvgReviewComments, r.AvgIssueComments)
		fmt.Fprintf(tw, "Estimated comments:\t%d (review_bodies=%d review_comments=%d issue_comments=%d)\n",
			r.EstTotalComments, r.EstReviewBodies, r.EstReviewComments, r.EstIssueComments)
		fmt.Fprintf(tw, "Estimated API calls:\t%d\n", r.EstAPICalls)
		if r.RateLimitLimit > 0 {
			fmt.Fprintf(tw, "Rate limit (core):\t%d/%d remaining (resets %s)\n",
				r.RateLimitRemaining, r.RateLimitLimit, r.RateLimitResetAt.Format("2006-01-02 15:04 MST"))
		} else {
			fmt.Fprintln(tw, "Rate limit (core):\tunknown")
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		for _, msg := range r.Warnings {
			fmt.Fprintf(w, "WARNING: %s\n", msg)
		}
		return nil
	default:
		return fmt.Errorf("unknown --format %q (allowed: text, json)", format)
	}
}
