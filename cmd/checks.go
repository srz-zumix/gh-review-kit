package cmd

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

type ChecksOptions struct {
	Exporter cmdutil.Exporter
}

// NewChecksCmd creates a new command to list check runs for a pull request
func NewChecksCmd() *cobra.Command {
	var (
		repo       string
		status     string
		conclusion string
		headers    []string
		colorFlag  string
		all        bool
		details    bool
		required   cmdflags.MutuallyExclusiveBoolFlags
		opts       ChecksOptions
	)

	cmd := &cobra.Command{
		Use:   "checks <pull-request-number>",
		Short: "List check runs for a pull request",
		Long: `List check runs for a pull request.

This command is similar to 'gh pr checks' but allows filtering by status.
You can also output run IDs and job IDs for use with 'gh run view'.`,
		Aliases: []string{"cc", "check-checks"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prNumber := args[0]

			repository, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			ctx := context.Background()

			// Get pull request
			pr, err := gh.GetPullRequest(ctx, client, repository, prNumber)
			if err != nil {
				return fmt.Errorf("failed to get pull request #%s: %w", prNumber, err)
			}

			// Get check runs for the PR head SHA
			filterOptions := &gh.ListChecksRunFilterOptions{}
			if status != "" {
				filterOptions.Status = &status
			}
			if conclusion != "" {
				filterOptions.Conclusion = &conclusion
			}
			filterOptions.Required = required.GetValue()
			filter := "latest"
			if all {
				filter = "all"
			}
			filterOptions.Filter = &filter

			results, err := gh.ListCheckRunsForRefWithGraphQL(ctx, client, repository, pr.GetHead().GetSHA(), pr.GetNumber(), filterOptions)
			// results, err := gh.ListCheckRunsForRef(ctx, client, repository, pr.GetHead().GetSHA(), filterOptions)
			if err != nil {
				return fmt.Errorf("failed to get check runs for pull request #%s: %w", prNumber, err)
			}

			gh.SortCheckRunsByName(results.CheckRuns)

			// Display check runs
			renderer := render.NewRenderer(opts.Exporter)
			renderer.SetColor(colorFlag)

			if len(headers) == 0 {
				if details {
					renderer.RenderCheckRunsDetails(results.CheckRuns)
				} else {
					renderer.RenderCheckRunsDefault(results.CheckRuns)
				}
			} else {
				renderer.RenderCheckRuns(results.CheckRuns, headers)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&all, "all", false, "Show all check runs, including those without a conclusion")
	f.BoolVarP(&details, "details", "d", false, "Show detailed information (status icon, run ID, job ID, timestamps, URLs)")
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	cmdutil.StringEnumFlag(cmd, &status, "status", "s", "", gh.ChecksRunFilterStatuses, "Filter by status")
	cmdutil.StringEnumFlag(cmd, &conclusion, "conclusion", "c", "", gh.ChecksRunFilterConclusions, "Filter by conclusion")
	f.StringSliceVarP(&headers, "headers", "H", []string{}, "Columns to display (NAME, STATUS, CONCLUSION, RUN_ID, JOB_ID, etc.)")
	required.AddNoPrefixFlag(cmd, "required", "Show only required check runs", "Show only non-required check runs")
	cmdutil.StringEnumFlag(cmd, &colorFlag, "color", "", render.ColorFlagAuto, render.ColorFlags, "Use color in diff output")
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}

func init() {
	rootCmd.AddCommand(NewChecksCmd())
}
