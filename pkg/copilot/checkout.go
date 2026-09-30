package copilot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/cli/cli/v2/git"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gitutil"
)

// PRHead identifies the head of a pull request that a local work tree is checked against.
type PRHead struct {
	Number int
	Ref    string
	SHA    string
	// Repos are the repositories whose git remotes identify a local clone of
	// the pull request, typically its base and (for forks) head repository.
	Repos []repository.Repository
}

// LocalCheckout describes a local work tree verified to be on a pull
// request's head branch and to contain its latest commit.
type LocalCheckout struct {
	Branch string
	SHA    string
}

// CheckLocalCheckout verifies that the current directory's git work tree is on
// head's branch and that HEAD contains head.SHA. It returns nil without error
// when the current directory is not a git work tree of any of head.Repos, since
// there is then no local checkout to check.
func CheckLocalCheckout(ctx context.Context, head PRHead) (*LocalCheckout, error) {
	c := gitutil.NewClient()

	inside, err := isInsideWorkTree(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("failed to check whether the current directory is inside a git work tree: %w", err)
	}
	if !inside {
		return nil, nil
	}
	remotes, err := c.Remotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list git remotes: %w", err)
	}
	if !remotesMatchAny(remotes, head.Repos) {
		return nil, nil
	}

	branch, err := c.CurrentBranch(ctx)
	if err != nil {
		if errors.Is(err, git.ErrNotOnAnyBranch) {
			return nil, fmt.Errorf("HEAD is detached, not on the pull request's head branch '%s'", head.Ref)
		}
		return nil, fmt.Errorf("failed to get the current branch: %w", err)
	}
	cfg, err := c.ReadBranchConfig(ctx, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to read the config of branch '%s': %w", branch, err)
	}
	if !branchMatches(branch, cfg.MergeRef, head) {
		return nil, fmt.Errorf("current branch '%s' is not the pull request's head branch '%s'", branch, head.Ref)
	}

	exists, err := gitutil.IsCommitObjectExists(ctx, c, head.SHA)
	if err != nil {
		return nil, fmt.Errorf("failed to look up commit %s: %w", head.SHA, err)
	}
	if !exists {
		return nil, fmt.Errorf("the pull request's latest commit %s is not in the local repository; pull the branch first", head.SHA)
	}
	contains, err := isAncestorOfHEAD(ctx, c, head.SHA)
	if err != nil {
		return nil, fmt.Errorf("failed to check whether HEAD contains commit %s: %w", head.SHA, err)
	}
	if !contains {
		return nil, fmt.Errorf("current branch '%s' does not contain the pull request's latest commit %s; pull the branch first", branch, head.SHA)
	}

	return &LocalCheckout{Branch: branch, SHA: head.SHA}, nil
}

// branchMatches reports whether a local branch tracks head's branch, either by
// name or through an upstream set by e.g. 'gh pr checkout' for fork branches.
func branchMatches(branch, mergeRef string, head PRHead) bool {
	if head.Ref != "" && branch == head.Ref {
		return true
	}
	switch mergeRef {
	case "":
		return false
	case "refs/heads/" + head.Ref, fmt.Sprintf("refs/pull/%d/head", head.Number):
		return true
	}
	return false
}

// isInsideWorkTree reports whether the git client's directory is inside a git
// work tree. Only git's "not a git repository" failure is reported as outside;
// any other failure is returned as an error so that the checkout check does not
// silently pass.
func isInsideWorkTree(ctx context.Context, c *git.Client) (bool, error) {
	cmd, err := c.Command(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, err
	}
	// Force untranslated messages so that the "not a git repository" check works under any locale.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		if isNotGitRepositoryError(err) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// isNotGitRepositoryError reports whether err is git's fatal "not a git
// repository" failure, which git exits with code 128 for.
func isNotGitRepositoryError(err error) bool {
	ge, ok := errors.AsType[*git.GitError](err)
	return ok && ge.ExitCode == 128 && strings.Contains(strings.ToLower(ge.Stderr), "not a git repository")
}

// isAncestorOfHEAD reports whether sha is reachable from HEAD.
func isAncestorOfHEAD(ctx context.Context, c *git.Client, sha string) (bool, error) {
	cmd, err := c.Command(ctx, "merge-base", "--is-ancestor", sha, "HEAD")
	if err != nil {
		return false, err
	}
	if err := cmd.Run(); err != nil {
		// Exit code 1 means "not an ancestor"; anything else is a real failure.
		if ge, ok := errors.AsType[*git.GitError](err); ok && ge.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// remotesMatchAny reports whether any fetch or push URL of remotes points at one of repos.
func remotesMatchAny(remotes git.RemoteSet, repos []repository.Repository) bool {
	for _, remote := range remotes {
		for _, repo := range repos {
			if remoteURLMatches(remote.FetchURL, repo) || remoteURLMatches(remote.PushURL, repo) {
				return true
			}
		}
	}
	return false
}

// remoteURLMatches reports whether u points at repo.
func remoteURLMatches(u *url.URL, repo repository.Repository) bool {
	if u == nil || repo.Owner == "" || repo.Name == "" {
		return false
	}
	parsed, err := repository.Parse(u.String())
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Owner, repo.Owner) &&
		strings.EqualFold(parsed.Name, repo.Name) &&
		(repo.Host == "" || strings.EqualFold(parsed.Host, repo.Host))
}
