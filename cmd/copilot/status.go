package copilot

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	pkgcopilot "github.com/srz-zumix/gh-review-kit/pkg/copilot"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewStatusCmd creates a new command to report the GitHub Copilot code review status of a pull request.
func NewStatusCmd() *cobra.Command {
	var (
		repo string
		opts struct{ Exporter cmdutil.Exporter }
	)

	cmd := &cobra.Command{
		Use:   "status [pull-request-number]",
		Short: "Show the GitHub Copilot code review status of a pull request",
		Long: `Show where GitHub Copilot's code review of a pull request stands.

The reported status is one of:

  not_requested      Copilot has not been requested and has not reviewed yet
  in_progress        Copilot is a requested reviewer and has not answered yet
  commented          Copilot's latest review left comments
  approved           Copilot's latest review approved the pull request
  changes_requested  Copilot's latest review requested changes
  dismissed          Copilot's latest review was dismissed

A pending review request takes precedence, so a pull request that Copilot has
already reviewed and is reviewing again is reported as in_progress.

If pull-request-number is omitted, the pull request for the current branch is used.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prIdentifier := ""
			if len(args) > 0 {
				prIdentifier = args[0]
			}

			repository, err := parser.Repository(parser.RepositoryInput(repo), parser.RepositoryFromURL(prIdentifier))
			if err != nil {
				return fmt.Errorf("failed to resolve repository: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(repository)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			ctx := context.Background()

			pr, err := gh.FindPRByIdentifier(ctx, client, repository, prIdentifier)
			if err != nil {
				return fmt.Errorf("failed to get pull request %s: %w", prIdentifier, err)
			}

			status, err := pkgcopilot.GetReviewStatus(ctx, client, repository, pr)
			if err != nil {
				return fmt.Errorf("failed to get the Copilot review status of pull request #%d: %w", pr.GetNumber(), err)
			}

			return pkgcopilot.RenderReviewStatus(render.NewRenderer(opts.Exporter), status)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}
