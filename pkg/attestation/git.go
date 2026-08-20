package attestation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/cli/cli/v2/git"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gitutil"
)

// GitTagCommit is the metadata key holding the full HEAD commit SHA.
const GitTagCommit = "git.commit"

// GitTagBranch is the metadata key holding the current branch name, or
// "detached" when HEAD does not point at any branch.
const GitTagBranch = "git.branch"

// GitTagDirty is the metadata key holding "true" or "false" depending on
// whether the working tree has uncommitted changes (including untracked
// files).
const GitTagDirty = "git.dirty"

// GitTagCommitDate is the metadata key holding the HEAD commit's committer
// date in RFC 3339 format.
const GitTagCommitDate = "git.commit_date"

// GitTagRepository is the metadata key holding the credential-free
// "host/owner/repo" identifier for the repository, or the top-level
// directory name when no "origin" remote is configured.
const GitTagRepository = "git.repository"

// DetachedBranch is the value used for GitMetadata.Branch when HEAD is not
// on any branch (detached HEAD state).
const DetachedBranch = "detached"

// GitMetadata holds the Git provenance information collected from a
// repository, ready to be embedded as video metadata tags.
type GitMetadata struct {
	Commit     string
	Branch     string
	Dirty      bool
	CommitDate time.Time
	Repository string
}

// Tag is an ordered key/value pair to embed as a metadata tag.
type Tag struct {
	Key   string
	Value string
}

// Tags returns the metadata tags to embed, in a stable, deterministic order.
func (m GitMetadata) Tags() []Tag {
	return []Tag{
		{Key: GitTagCommit, Value: m.Commit},
		{Key: GitTagBranch, Value: m.Branch},
		{Key: GitTagDirty, Value: dirtyValue(m.Dirty)},
		{Key: GitTagCommitDate, Value: m.CommitDate.Format(time.RFC3339)},
		{Key: GitTagRepository, Value: m.Repository},
	}
}

func dirtyValue(dirty bool) string {
	if dirty {
		return "true"
	}
	return "false"
}

// CollectGitMetadata collects Git provenance information from the
// repository at repoDir. When repoDir is empty, the current directory is
// used.
func CollectGitMetadata(ctx context.Context, repoDir string) (*GitMetadata, error) {
	c := gitutil.ClientForDir(repoDir)

	if err := verifyWorkTree(ctx, c); err != nil {
		return nil, err
	}

	commit, err := headCommit(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve HEAD commit: %w", err)
	}

	branch, err := currentBranch(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve current branch: %w", err)
	}

	dirty, err := isDirty(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to determine working tree status: %w", err)
	}

	commitDate, err := headCommitDate(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve HEAD commit date: %w", err)
	}

	repo, err := repositoryIdentifier(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository identifier: %w", err)
	}

	return &GitMetadata{
		Commit:     commit,
		Branch:     branch,
		Dirty:      dirty,
		CommitDate: commitDate,
		Repository: repo,
	}, nil
}

// verifyWorkTree checks that the target directory is inside a Git work tree.
func verifyWorkTree(ctx context.Context, c *git.Client) error {
	cmd, err := c.Command(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("failed to prepare git command: %w", err)
	}
	if _, err := cmd.Output(); err != nil {
		return fmt.Errorf("not a git work tree: %w", err)
	}
	return nil
}

// headCommit returns the full SHA of the HEAD commit.
func headCommit(ctx context.Context, c *git.Client) (string, error) {
	cmd, err := c.Command(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// currentBranch returns the current branch name, or DetachedBranch when HEAD
// is detached.
func currentBranch(ctx context.Context, c *git.Client) (string, error) {
	branch, err := c.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrNotOnAnyBranch) {
			return DetachedBranch, nil
		}
		return "", err
	}
	return branch, nil
}

// isDirty reports whether the working tree has uncommitted changes,
// including untracked files.
func isDirty(ctx context.Context, c *git.Client) (bool, error) {
	cmd, err := c.Command(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// headCommitDate returns the committer date of the HEAD commit in RFC 3339
// format.
func headCommitDate(ctx context.Context, c *git.Client) (time.Time, error) {
	cmd, err := c.Command(ctx, "-c", "log.ShowSignature=false", "show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return time.Time{}, err
	}
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, err
	}
	value := strings.TrimSpace(string(out))
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse commit date %q: %w", value, err)
	}
	return t, nil
}

// repositoryIdentifier returns the credential-free "host/owner/repo"
// identifier derived from the "origin" remote, or the top-level directory
// name when no "origin" remote is configured.
func repositoryIdentifier(ctx context.Context, c *git.Client) (string, error) {
	cmd, err := c.Command(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return topLevelDirName(ctx, c)
	}
	remoteURL := strings.TrimSpace(string(out))
	if remoteURL == "" {
		return topLevelDirName(ctx, c)
	}

	repo, err := repository.Parse(remoteURL)
	if err != nil {
		return topLevelDirName(ctx, c)
	}
	name := strings.TrimSuffix(repo.Name, ".git")
	return fmt.Sprintf("%s/%s/%s", repo.Host, repo.Owner, name), nil
}

// topLevelDirName returns the base name of the repository's top-level
// directory, used as a fallback repository identifier.
func topLevelDirName(ctx context.Context, c *git.Client) (string, error) {
	top, err := c.ToplevelDir(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repository top-level directory: %w", err)
	}
	return filepath.Base(top), nil
}
