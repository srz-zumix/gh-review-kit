package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-review-kit/pkg/checks"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewFlushFailureCmd creates a new command to display logs for failed check runs
func NewFlushFailureCmd() *cobra.Command {
	var (
		repo     string
		required cmdflags.MutuallyExclusiveBoolFlags
		full     bool
	)

	cmd := &cobra.Command{
		Use:   "flush-failure <pull-request-number>",
		Short: "Display logs for failed check runs",
		Long: `Display logs for failed check runs in a pull request.

This command retrieves all check runs with 'failure' conclusion and displays their logs
using 'gh run view --log' for each failed check run.`,
		Aliases: []string{"ff", "flush-fail", "flush-failed"},
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
			conclusion := gh.ChecksRunConclusionFailure
			filterOptions.Conclusion = &conclusion
			filter := "latest"
			filterOptions.Filter = &filter
			filterOptions.Required = required.GetValue()

			results, err := gh.ListCheckRunsForRefWithGraphQL(ctx, client, repository, pr.GetHead().GetSHA(), pr.GetNumber(), filterOptions)
			if err != nil {
				return fmt.Errorf("failed to get check runs for pull request #%s: %w", prNumber, err)
			}

			if len(results.CheckRuns) == 0 {
				logger.Info("No failed check runs found", "pull_request", pr.GetNumber())
				return nil
			}

			runsCount := len(results.CheckRuns)
			logger.Info("Found failed check runs", "count", runsCount)

			// Display logs for each failed check run
			for i, checkRun := range results.CheckRuns {
				checkRunID := checkRun.GetID()
				if checkRunID == 0 {
					logger.Warn("Could not get check run ID", "name", checkRun.GetName())
					continue
				}

				logger.Info("Check Run", "index", fmt.Sprintf("%d/%d", i+1, runsCount), "name", checkRun.GetName(), "id", checkRunID)

				// Get log content using GetCheckRunJobLogsContent
				if full {
					logContent, err := gh.GetCheckRunJobLogsContent(ctx, client, repository, checkRunID, 3)
					if err != nil {
						logger.Warn("Failed to get logs for check run", "name", checkRun.GetName(), "id", checkRunID, "error", err)
						continue
					}

					// Display log content
					os.Stdout.Write(logContent)
					fmt.Fprintf(os.Stdout, "\n")
				} else {
					workflowJob, err := gh.GetWorkflowJobByID(ctx, client, repository, checkRun.GetID())
					if err != nil {
						logger.Warn("Failed to get workflow job for check run", "name", checkRun.GetName(), "id", checkRunID, "error", err)
						continue
					}

					walkier := checks.NewRunLogWalker(ctx, client, repository, workflowJob)
					if err := walkier.Fetch(3); err != nil {
						logger.Warn("Failed to fetch logs for workflow job", "job_id", workflowJob.GetID(), "error", err)
						continue
					}

					err = walkier.Walk(*workflowJob, func(step *checks.TaskStep, stepLog *checks.StepLog) error {
						if step.GetConclusion() == gh.ChecksRunConclusionFailure {
							content, err := stepLog.ReadContent()
							if err != nil {
								logger.Warn("Failed to read step log content", "step", step.GetName(), "error", err)
								return nil
							}
							fmt.Fprintf(os.Stdout, "%s\n", content)
						}
						return nil
					})
					if err != nil {
						logger.Warn("Failed to walk through logs for workflow job", "job_id", workflowJob.GetID(), "error", err)
						continue
					}
				}
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&full, "full", false, "Display full logs")
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	required.AddNoPrefixFlag(cmd, "required", "Show only required check runs", "Show only non-required check runs")

	return cmd
}

func init() {
	rootCmd.AddCommand(NewFlushFailureCmd())
}
