package copilot

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// initCheckoutRepo creates a repository with origin pointing at owner/repo,
// checked out on branch "feature" with two commits, and chdirs into it.
func initCheckoutRepo(t *testing.T) (dir, first, second string) {
	t.Helper()
	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "feature")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	first = runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "second")
	second = runGit(t, dir, "rev-parse", "HEAD")
	t.Chdir(dir)
	return dir, first, second
}

func testHead(sha string) PRHead {
	return PRHead{
		Number: 7,
		Ref:    "feature",
		SHA:    sha,
		Repos:  []repository.Repository{{Host: "github.com", Owner: "owner", Name: "repo"}},
	}
}

func TestCheckLocalCheckoutOK(t *testing.T) {
	_, first, second := initCheckoutRepo(t)
	for _, sha := range []string{second, first} {
		got, err := CheckLocalCheckout(context.Background(), testHead(sha))
		if err != nil {
			t.Fatalf("CheckLocalCheckout(%s) error = %v", sha, err)
		}
		if got == nil || got.Branch != "feature" || got.SHA != sha {
			t.Errorf("CheckLocalCheckout(%s) = %+v, want branch feature and sha %s", sha, got, sha)
		}
	}
}

func TestCheckLocalCheckoutBehind(t *testing.T) {
	dir, first, second := initCheckoutRepo(t)
	runGit(t, dir, "reset", "-q", "--hard", first)
	if _, err := CheckLocalCheckout(context.Background(), testHead(second)); err == nil {
		t.Fatal("CheckLocalCheckout() error = nil, want error for a branch behind the pull request head")
	}
}

func TestCheckLocalCheckoutMissingCommit(t *testing.T) {
	initCheckoutRepo(t)
	if _, err := CheckLocalCheckout(context.Background(), testHead(strings.Repeat("a", 40))); err == nil {
		t.Fatal("CheckLocalCheckout() error = nil, want error for an unfetched commit")
	}
}

func TestCheckLocalCheckoutWrongBranch(t *testing.T) {
	dir, _, second := initCheckoutRepo(t)
	runGit(t, dir, "checkout", "-q", "-b", "other")
	if _, err := CheckLocalCheckout(context.Background(), testHead(second)); err == nil {
		t.Fatal("CheckLocalCheckout() error = nil, want error for a different branch")
	}
}

func TestCheckLocalCheckoutDetached(t *testing.T) {
	dir, _, second := initCheckoutRepo(t)
	runGit(t, dir, "checkout", "-q", "--detach")
	if _, err := CheckLocalCheckout(context.Background(), testHead(second)); err == nil {
		t.Fatal("CheckLocalCheckout() error = nil, want error for a detached HEAD")
	}
}

func TestCheckLocalCheckoutSkipsUnrelatedRepository(t *testing.T) {
	_, _, second := initCheckoutRepo(t)
	head := testHead(second)
	head.Repos = []repository.Repository{{Host: "github.com", Owner: "other", Name: "repo"}}
	got, err := CheckLocalCheckout(context.Background(), head)
	if err != nil || got != nil {
		t.Errorf("CheckLocalCheckout() = %+v, %v, want nil, nil for an unrelated repository", got, err)
	}
}

func TestCheckLocalCheckoutSkipsOutsideWorkTree(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := CheckLocalCheckout(context.Background(), testHead(strings.Repeat("a", 40)))
	if err != nil || got != nil {
		t.Errorf("CheckLocalCheckout() = %+v, %v, want nil, nil outside a work tree", got, err)
	}
}

func TestBranchMatches(t *testing.T) {
	head := PRHead{Number: 7, Ref: "feature"}
	tests := []struct {
		name     string
		branch   string
		mergeRef string
		want     bool
	}{
		{name: "same name", branch: "feature", want: true},
		{name: "upstream branch", branch: "local", mergeRef: "refs/heads/feature", want: true},
		{name: "upstream pull ref", branch: "owner-feature", mergeRef: "refs/pull/7/head", want: true},
		{name: "other pull ref", branch: "owner-feature", mergeRef: "refs/pull/8/head", want: false},
		{name: "unrelated", branch: "main", mergeRef: "refs/heads/main", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := branchMatches(tt.branch, tt.mergeRef, head); got != tt.want {
				t.Errorf("branchMatches(%q, %q) = %v, want %v", tt.branch, tt.mergeRef, got, tt.want)
			}
		})
	}
}

func TestCheckLocalCheckoutFailsOnBrokenRepository(t *testing.T) {
	dir, _, second := initCheckoutRepo(t)
	if err := os.WriteFile(dir+"/.git/config", []byte("[core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := CheckLocalCheckout(context.Background(), testHead(second))
	if err == nil {
		t.Errorf("CheckLocalCheckout() = %+v, nil, want error for a broken repository", got)
	}
}
