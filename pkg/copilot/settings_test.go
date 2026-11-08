package copilot

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestBuildRepositorySettingsUpdate(t *testing.T) {
	tests := []struct {
		name    string
		in      RepositorySettingsChanges
		wantErr string
	}{
		{name: "no changes", in: RepositorySettingsChanges{}, wantErr: "no setting to change"},
		{name: "empty effort", in: RepositorySettingsChanges{Effort: strPtr(" ")}, wantErr: "must not be empty"},
		{name: "invalid severity", in: RepositorySettingsChanges{SeverityFilter: strPtr("high")}, wantErr: "invalid --severity-filter"},
		{name: "effort only", in: RepositorySettingsChanges{Effort: strPtr("max")}},
		{name: "inherit", in: RepositorySettingsChanges{Effort: strPtr(gh.CopilotCodeReviewEffortInherit)}},
		{name: "all", in: RepositorySettingsChanges{Effort: strPtr("lite"), SeverityFilter: strPtr("low"), CustomInstructions: boolPtr(false), MCP: boolPtr(true)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildRepositorySettingsUpdate(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.ReviewEffort != tt.in.Effort || got.SeverityFilter != tt.in.SeverityFilter ||
				got.CustomInstructionsEnabled != tt.in.CustomInstructions || got.MCPEnabled != tt.in.MCP {
				t.Errorf("update does not mirror changes: %+v", got)
			}
		})
	}
}

func TestBuildOrganizationSettingsUpdate(t *testing.T) {
	if _, err := BuildOrganizationSettingsUpdate(RepositorySettingsChanges{Effort: strPtr("max"), MCP: boolPtr(true)}); err == nil {
		t.Error("expected error for repository-only flag")
	}
	if _, err := BuildOrganizationSettingsUpdate(RepositorySettingsChanges{}); err == nil {
		t.Error("expected error when --effort is missing")
	}
	if _, err := BuildOrganizationSettingsUpdate(RepositorySettingsChanges{Effort: strPtr("")}); err == nil {
		t.Error("expected error for empty --effort")
	}
	got, err := BuildOrganizationSettingsUpdate(RepositorySettingsChanges{Effort: strPtr("balanced")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.DefaultReviewEffort == nil || *got.DefaultReviewEffort != "balanced" {
		t.Errorf("DefaultReviewEffort = %v", got.DefaultReviewEffort)
	}
}

func TestRenderRepositorySettings(t *testing.T) {
	sr := render.NewStringRenderer(nil)
	s := &gh.RepositoryCopilotCodeReviewSettings{
		ReviewEffort:              gh.RepositoryCopilotCodeReviewEffort{Effective: "balanced"},
		AvailableReviewEfforts:    []string{"lite", "balanced"},
		SeverityFilter:            "no_filter",
		CustomInstructionsEnabled: true,
	}
	if err := RenderRepositorySettings(&sr.Renderer, s); err != nil {
		t.Fatal(err)
	}
	want := "review_effort=balanced\nreview_effort_configured=\navailable_review_efforts=lite,balanced\nseverity_filter=no_filter\ncustom_instructions_enabled=true\nmcp_enabled=false\n"
	if sr.Stdout.String() != want {
		t.Errorf("output =\n%s\nwant\n%s", sr.Stdout.String(), want)
	}
}

func TestResolveSettingsTargets(t *testing.T) {
	r, err := ResolveRepositoryOrOrganization("example-org/example-repo", "")
	if err != nil || r.Owner != "example-org" || r.Name != "example-repo" {
		t.Errorf("repository target = %+v, %v", r, err)
	}
	r, err = ResolveRepositoryOrOrganization("", "ghe.example.com/example-org")
	if err != nil || r.Owner != "example-org" || r.Name != "" || r.Host != "ghe.example.com" {
		t.Errorf("organization target = %+v, %v", r, err)
	}
}

func TestWrapAdminAPIError(t *testing.T) {
	forbidden := &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusForbidden}, Message: "Forbidden"}
	if err := WrapAdminAPIError(forbidden); !strings.Contains(err.Error(), "organization Copilot code review endpoints") ||
		!strings.Contains(err.Error(), "fine-grained") || strings.Contains(err.Error(), "enterprise") || !errors.Is(err, forbidden) {
		t.Errorf("forbidden error = %v", err)
	}
	notFound := &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}, Message: "Not Found"}
	if err := WrapAdminAPIError(notFound); err != notFound {
		t.Errorf("non-403 error should be returned unchanged, got %v", err)
	}
}
