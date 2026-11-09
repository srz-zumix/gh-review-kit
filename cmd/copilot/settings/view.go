package settings

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	pkgcopilot "github.com/srz-zumix/gh-review-kit/pkg/copilot"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewViewCmd creates a command to show Copilot code review settings.
func NewViewCmd() *cobra.Command {
	var (
		repo string
		org  string
		opts struct{ Exporter cmdutil.Exporter }
	)

	cmd := &cobra.Command{
		Use:   "view",
		Short: "Show Copilot code review settings of a repository or organization",
		Long: `Show the Copilot code review settings of a repository or an organization.

For a repository, the output includes the configured and effective review effort,
the available review efforts, the severity filter, and whether custom
instructions and MCP servers are used. With --org, the output shows the
organization's default review effort and who manages it.

Organization settings require a classic personal access token or OAuth token;
GitHub App and fine-grained tokens are not supported.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := pkgcopilot.ResolveRepositoryOrOrganization(repo, org)
			if err != nil {
				return fmt.Errorf("failed to resolve repository or organization: %w", err)
			}
			client, err := gh.NewGitHubClientWithRepo(target)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}
			ctx := context.Background()
			r := render.NewRenderer(opts.Exporter)

			if org != "" {
				s, err := gh.GetOrganizationCopilotCodeReviewSettings(ctx, client, target)
				if err != nil {
					return fmt.Errorf("failed to get Copilot code review settings of organization %s: %w", target.Owner, pkgcopilot.WrapAdminAPIError(err))
				}
				return pkgcopilot.RenderOrganizationSettings(r, s)
			}
			s, err := gh.GetRepositoryCopilotCodeReviewSettings(ctx, client, target)
			if err != nil {
				return fmt.Errorf("failed to get Copilot code review settings of repository %s/%s: %w", target.Owner, target.Name, err)
			}
			return pkgcopilot.RenderRepositorySettings(r, s)
		},
	}

	f := cmd.Flags()
	f.StringVar(&org, "org", "", "Organization in the format '[HOST/]ORG'")
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	cmd.MarkFlagsMutuallyExclusive("org", "repo")
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}
