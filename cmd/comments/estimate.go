package comments

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/comments"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
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
		exporter     cmdutil.Exporter
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
			types, err := comments.CommentTypesFromStrings(commentTypes)
			if err != nil {
				return fmt.Errorf("invalid --comment-types: %w", err)
			}

			result, err := comments.Estimate(context.Background(), client, comments.EstimateOptions{
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
			return writeEstimateOutput(render.NewRenderer(exporter), result)
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
	if err := cmdflags.AddFormatFlags(cmd, &exporter, &format, "text", []string{"text"}); err != nil {
		panic(err)
	}
	return cmd
}

func writeEstimateOutput(r *render.Renderer, result *comments.EstimateResult) error {
	if r.HasExporter() {
		return r.RenderExportedData(result)
	}
	tw := r.NewTableWriter([]string{"FIELD", "VALUE"})
	tw.Append([]string{"Repo", result.Repo})
	tw.Append([]string{"Matched PRs", strconv.Itoa(result.PRCount)})
	tw.Append([]string{"Sampled PRs", strconv.Itoa(result.SampledPRs)})
	tw.Append([]string{"Avg per PR", fmt.Sprintf("review_bodies=%.2f review_comments=%.2f issue_comments=%.2f",
		result.AvgReviewBodies, result.AvgReviewComments, result.AvgIssueComments)})
	tw.Append([]string{"Estimated comments", fmt.Sprintf("%d (review_bodies=%d review_comments=%d issue_comments=%d)",
		result.EstTotalComments, result.EstReviewBodies, result.EstReviewComments, result.EstIssueComments)})
	tw.Append([]string{"Estimated API calls", strconv.Itoa(result.EstAPICalls)})
	if result.RateLimitLimit > 0 {
		tw.Append([]string{"Rate limit (core)", fmt.Sprintf("%d/%d remaining (resets %s)",
			result.RateLimitRemaining, result.RateLimitLimit, result.RateLimitResetAt.Format("2006-01-02 15:04 MST"))})
	} else {
		tw.Append([]string{"Rate limit (core)", "unknown"})
	}
	if err := tw.Render(); err != nil {
		return err
	}
	for _, msg := range result.Warnings {
		r.WriteLine("WARNING: " + msg)
	}
	return nil
}
