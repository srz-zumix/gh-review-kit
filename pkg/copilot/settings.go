package copilot

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// ResolveRepositoryOrOrganization resolves the target of a settings command.
// It returns an organization (empty Name) when org is set, and otherwise the
// repository given by repo or the current repository.
func ResolveRepositoryOrOrganization(repo, org string) (repository.Repository, error) {
	if org != "" {
		return parser.Repository(parser.RepositoryOwnerWithHost(org))
	}
	return parser.Repository(parser.RepositoryInput(repo))
}

// WrapAdminAPIError adds a hint to a 403 response from an organization Copilot
// code review endpoint, which rejects GitHub App and fine-grained personal
// access tokens.
func WrapAdminAPIError(err error) error {
	if gh.IsHTTPForbidden(err) {
		return fmt.Errorf("%w (organization Copilot code review endpoints require a classic personal access token or OAuth token of an owner; GitHub App and fine-grained tokens are not supported)", err)
	}
	return err
}

// RepositorySettingsChanges holds the repository settings flags the user set.
// Nil fields were not given and are left unchanged.
type RepositorySettingsChanges struct {
	Effort             *string
	SeverityFilter     *string
	CustomInstructions *bool
	MCP                *bool
}

// BuildRepositorySettingsUpdate validates changes and converts them into the
// update body for a repository.
func BuildRepositorySettingsUpdate(c RepositorySettingsChanges) (*gh.RepositoryCopilotCodeReviewSettingsUpdate, error) {
	if c.Effort == nil && c.SeverityFilter == nil && c.CustomInstructions == nil && c.MCP == nil {
		return nil, fmt.Errorf("no setting to change; specify at least one of --effort, --severity-filter, --custom-instructions, or --mcp")
	}
	if c.Effort != nil && strings.TrimSpace(*c.Effort) == "" {
		return nil, fmt.Errorf("--effort must not be empty; use %q to clear the configured value", gh.CopilotCodeReviewEffortInherit)
	}
	if c.SeverityFilter != nil && !slices.Contains(gh.CopilotCodeReviewSeverityFilters, *c.SeverityFilter) {
		return nil, fmt.Errorf("invalid --severity-filter %q: must be one of %s", *c.SeverityFilter, strings.Join(gh.CopilotCodeReviewSeverityFilters, ", "))
	}
	return &gh.RepositoryCopilotCodeReviewSettingsUpdate{
		ReviewEffort:              c.Effort,
		SeverityFilter:            c.SeverityFilter,
		CustomInstructionsEnabled: c.CustomInstructions,
		MCPEnabled:                c.MCP,
	}, nil
}

// BuildOrganizationSettingsUpdate validates changes and converts them into the
// update body for an organization, which only supports the default review effort.
func BuildOrganizationSettingsUpdate(c RepositorySettingsChanges) (*gh.OrganizationCopilotCodeReviewSettingsUpdate, error) {
	if c.SeverityFilter != nil || c.CustomInstructions != nil || c.MCP != nil {
		return nil, fmt.Errorf("--severity-filter, --custom-instructions, and --mcp are repository settings and cannot be used with --org")
	}
	if c.Effort == nil {
		return nil, fmt.Errorf("no setting to change; specify --effort")
	}
	if strings.TrimSpace(*c.Effort) == "" {
		return nil, fmt.Errorf("--effort must not be empty; use %q to clear the configured value", gh.CopilotCodeReviewEffortInherit)
	}
	return &gh.OrganizationCopilotCodeReviewSettingsUpdate{DefaultReviewEffort: c.Effort}, nil
}

func valueOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RenderRepositorySettings writes repository settings as "key=value" lines, or
// as exported data when r has an exporter.
func RenderRepositorySettings(r *render.Renderer, s *gh.RepositoryCopilotCodeReviewSettings) error {
	if r.HasExporter() {
		return r.RenderExportedData(s)
	}
	r.WriteLine(fmt.Sprintf("review_effort=%s", s.ReviewEffort.Effective))
	r.WriteLine(fmt.Sprintf("review_effort_configured=%s", valueOrEmpty(s.ReviewEffort.Configured)))
	r.WriteLine(fmt.Sprintf("available_review_efforts=%s", strings.Join(s.AvailableReviewEfforts, ",")))
	r.WriteLine(fmt.Sprintf("severity_filter=%s", s.SeverityFilter))
	r.WriteLine(fmt.Sprintf("custom_instructions_enabled=%t", s.CustomInstructionsEnabled))
	r.WriteLine(fmt.Sprintf("mcp_enabled=%t", s.MCPEnabled))
	return nil
}

// RenderOrganizationSettings writes organization settings as "key=value"
// lines, or as exported data when r has an exporter.
func RenderOrganizationSettings(r *render.Renderer, s *gh.OrganizationCopilotCodeReviewSettings) error {
	if r.HasExporter() {
		return r.RenderExportedData(s)
	}
	e := s.DefaultReviewEffort
	r.WriteLine(fmt.Sprintf("default_review_effort=%s", e.Effective))
	r.WriteLine(fmt.Sprintf("default_review_effort_configured=%s", valueOrEmpty(e.Configured)))
	r.WriteLine(fmt.Sprintf("github_default_review_effort=%s", e.GitHubDefault))
	r.WriteLine(fmt.Sprintf("editable=%t", e.Editable))
	r.WriteLine(fmt.Sprintf("managed_by=%s", e.ManagedBy))
	r.WriteLine(fmt.Sprintf("available_review_efforts=%s", strings.Join(s.AvailableReviewEfforts, ",")))
	return nil
}
