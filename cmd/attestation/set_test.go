package attestation

import (
	"context"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/gh-review-kit/pkg/attestation"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// withReadonly overrides the isReadonly seam for the duration of a test.
func withReadonly(t *testing.T, v bool) {
	t.Helper()
	orig := isReadonly
	isReadonly = func() bool { return v }
	t.Cleanup(func() { isReadonly = orig })
}

// runSet executes the set command with the given args and returns its error,
// discarding any output. updateAssets is stubbed so no GitHub API is called.
func runSet(t *testing.T, args ...string) error {
	t.Helper()
	origUpdate := updateAssets
	updateAssets = func(ctx context.Context, g *gh.GitHubClient, opts attestation.UpdateAssetsOptions) ([]*attestation.AssetUpdate, error) {
		return nil, nil
	}
	origClient := newGitHubClientWithRepo
	newGitHubClientWithRepo = func(repo repository.Repository) (*gh.GitHubClient, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		updateAssets = origUpdate
		newGitHubClientWithRepo = origClient
	})

	cmd := NewSetCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestSetRejectsBothPRAndIssue(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t, "--pr", "1", "--issue", "2")
	if err == nil || !strings.Contains(err.Error(), "both --pr and --issue") {
		t.Fatalf("expected mutually-exclusive error, got %v", err)
	}
}

func TestSetRequiresInputOrTarget(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t)
	if err == nil || !strings.Contains(err.Error(), "requires exactly one of") {
		t.Fatalf("expected missing-target error, got %v", err)
	}
}

func TestSetRejectsNegativeMaxAssetSize(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t, "--pr", "1", "--max-asset-size", "-1")
	if err == nil || !strings.Contains(err.Error(), "max-asset-size") {
		t.Fatalf("expected max-asset-size error, got %v", err)
	}
}

func TestSetLocalModeRejectsReadonly(t *testing.T) {
	withReadonly(t, true)
	err := runSet(t, "input.png")
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestSetPRModeRejectsReadonly(t *testing.T) {
	withReadonly(t, true)
	err := runSet(t, "--pr", "1")
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only error, got %v", err)
	}
}

func TestSetRejectsNonURLArgWithPR(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t, "--pr", "1", "local.png")
	if err == nil || !strings.Contains(err.Error(), "must be an asset URL") {
		t.Fatalf("expected asset-URL requirement error, got %v", err)
	}
}

func TestSetRejectsOutputWithoutAssetURL(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t, "--pr", "1", "--output", "out.png")
	if err == nil || !strings.Contains(err.Error(), "single <asset-url>") {
		t.Fatalf("expected output-requires-asset-url error, got %v", err)
	}
}

func TestSetRejectsNonGitHubAssetURL(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t, "--repo", "owner/repo", "--pr", "1", "https://example.com/x.png")
	if err == nil || !strings.Contains(err.Error(), "recognized GitHub-hosted asset URL") {
		t.Fatalf("expected non-GitHub asset URL error, got %v", err)
	}
}

func TestSetRejectsMalformedURLArg(t *testing.T) {
	withReadonly(t, false)
	err := runSet(t, "--pr", "1", "https://[::1")
	if err == nil {
		t.Fatal("expected error for malformed URL argument, got nil")
	}
}

// TestSetForwardsOptions verifies the --pr/--issue path maps flags and the
// target selection onto UpdateAssetsOptions exactly.
func TestSetForwardsOptions(t *testing.T) {
	withReadonly(t, false)

	const assetURL = "https://github.com/user-attachments/assets/00000000-0000-0000-0000-000000000000"

	cases := []struct {
		name        string
		targetFlag  string
		targetValue string
		wantPR      bool
	}{
		{"pr", "--pr", "feature/x", true},
		{"issue", "--issue", "456", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got attestation.UpdateAssetsOptions
			var called bool
			origUpdate := updateAssets
			updateAssets = func(ctx context.Context, g *gh.GitHubClient, opts attestation.UpdateAssetsOptions) ([]*attestation.AssetUpdate, error) {
				got, called = opts, true
				return nil, nil
			}
			origClient := newGitHubClientWithRepo
			newGitHubClientWithRepo = func(repo repository.Repository) (*gh.GitHubClient, error) {
				return nil, nil
			}
			t.Cleanup(func() {
				updateAssets = origUpdate
				newGitHubClientWithRepo = origClient
			})

			cmd := NewSetCmd()
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs([]string{
				"--repo", "owner/repo",
				tc.targetFlag, tc.targetValue,
				"--output", "out.png",
				"--comment", "hi",
				"--repo-dir", "/tmp/repo",
				"--max-asset-size", "5",
				"--force",
				assetURL,
			})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if !called {
				t.Fatal("updateAssets was not called")
			}
			if got.Repo.Owner != "owner" || got.Repo.Name != "repo" {
				t.Errorf("Repo = %s/%s, want owner/repo", got.Repo.Owner, got.Repo.Name)
			}
			if got.Target != tc.targetValue {
				t.Errorf("Target = %q, want %q", got.Target, tc.targetValue)
			}
			if got.PullRequest != tc.wantPR {
				t.Errorf("PullRequest = %v, want %v", got.PullRequest, tc.wantPR)
			}
			if got.AssetURL != assetURL {
				t.Errorf("AssetURL = %q, want %q", got.AssetURL, assetURL)
			}
			if got.Output != "out.png" {
				t.Errorf("Output = %q, want out.png", got.Output)
			}
			if got.Comment != "hi" {
				t.Errorf("Comment = %q, want hi", got.Comment)
			}
			if got.RepoDir != "/tmp/repo" {
				t.Errorf("RepoDir = %q, want /tmp/repo", got.RepoDir)
			}
			if got.MaxAssetSize != 5 {
				t.Errorf("MaxAssetSize = %d, want 5", got.MaxAssetSize)
			}
			if !got.Force {
				t.Error("Force = false, want true")
			}
		})
	}
}
