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

// NewSetCmd creates a command to change Copilot code review settings.
func NewSetCmd() *cobra.Command {
	var (
		repo               string
		org                string
		effort             string
		severityFilter     string
		customInstructions bool
		mcp                bool
		opts               struct{ Exporter cmdutil.Exporter }
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Change Copilot code review settings of a repository or organization",
		Long: `Change the Copilot code review settings of a repository or an organization.

Only the flags you pass are changed. For a repository you can set --effort,
--severity-filter, --custom-instructions, and --mcp. With --org only --effort is
accepted, and it sets the organization's default review effort.

--severity-filter chooses which comment severities Copilot leaves out: no_filter
keeps every comment, low leaves out low-severity comments, and low_and_medium
leaves out low- and medium-severity comments.

Pass --effort inherit to clear the configured value and fall back to the
inherited default. The updated settings are printed after the change.

Organization settings require a classic personal access token or OAuth token;
GitHub App and fine-grained tokens are not supported.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			changes := pkgcopilot.RepositorySettingsChanges{}
			f := cmd.Flags()
			if f.Changed("effort") {
				changes.Effort = &effort
			}
			if f.Changed("severity-filter") {
				changes.SeverityFilter = &severityFilter
			}
			if f.Changed("custom-instructions") {
				changes.CustomInstructions = &customInstructions
			}
			if f.Changed("mcp") {
				changes.MCP = &mcp
			}

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
				update, err := pkgcopilot.BuildOrganizationSettingsUpdate(changes)
				if err != nil {
					return err
				}
				s, err := gh.UpdateOrganizationCopilotCodeReviewSettings(ctx, client, target, update)
				if err != nil {
					return fmt.Errorf("failed to update Copilot code review settings of organization %s: %w", target.Owner, pkgcopilot.WrapAdminAPIError(err))
				}
				return pkgcopilot.RenderOrganizationSettings(r, s)
			}
			update, err := pkgcopilot.BuildRepositorySettingsUpdate(changes)
			if err != nil {
				return err
			}
			s, err := gh.UpdateRepositoryCopilotCodeReviewSettings(ctx, client, target, update)
			if err != nil {
				return fmt.Errorf("failed to update Copilot code review settings of repository %s/%s: %w", target.Owner, target.Name, err)
			}
			return pkgcopilot.RenderRepositorySettings(r, s)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&customInstructions, "custom-instructions", false, "Use repository custom instructions in Copilot code review")
	f.StringVarP(&effort, "effort", "e", "", "Review effort (e.g. lite, balanced, max), or 'inherit' to clear it")
	f.BoolVar(&mcp, "mcp", false, "Use MCP servers in Copilot code review")
	f.StringVar(&org, "org", "", "Organization in the format '[HOST/]ORG'")
	f.StringVarP(&repo, "repo", "R", "", "Repository in the format 'owner/repo'")
	f.StringVar(&severityFilter, "severity-filter", "", "Comment severities to leave out: no_filter, low, low_and_medium")
	cmd.MarkFlagsMutuallyExclusive("org", "repo")
	_ = cmd.RegisterFlagCompletionFunc("effort", cobra.FixedCompletions(append(append([]string{}, gh.CopilotCodeReviewEfforts...), gh.CopilotCodeReviewEffortInherit), cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("severity-filter", cobra.FixedCompletions(gh.CopilotCodeReviewSeverityFilters, cobra.ShellCompDirectiveNoFileComp))
	cmdutil.AddFormatFlags(cmd, &opts.Exporter)

	return cmd
}
