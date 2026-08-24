package attestation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-q", "-m", "initial commit")
	return dir
}

func TestCollectGitMetadataCleanBranch(t *testing.T) {
	dir := initRepo(t)
	wantSHA := runGit(t, dir, "rev-parse", "HEAD")

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Commit != wantSHA {
		t.Errorf("Commit: got %q want %q", meta.Commit, wantSHA)
	}
	if meta.Branch != "main" {
		t.Errorf("Branch: got %q want %q", meta.Branch, "main")
	}
	if meta.Dirty {
		t.Errorf("Dirty: got true want false")
	}
	if meta.CommitDate.IsZero() {
		t.Errorf("CommitDate: got zero value")
	}
	if meta.Repository != filepath.Base(dir) {
		t.Errorf("Repository: got %q want %q (fallback to top-level dir name)", meta.Repository, filepath.Base(dir))
	}
}

func TestCollectGitMetadataDirtyTracked(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if !meta.Dirty {
		t.Errorf("Dirty: got false want true (tracked file modified)")
	}
}

func TestCollectGitMetadataDirtyUntracked(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if !meta.Dirty {
		t.Errorf("Dirty: got false want true (untracked file present)")
	}
}

func TestCollectGitMetadataDetachedHead(t *testing.T) {
	dir := initRepo(t)
	sha := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "-q", sha)

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Branch != DetachedBranch {
		t.Errorf("Branch: got %q want %q", meta.Branch, DetachedBranch)
	}
}

func TestCollectGitMetadataRepositoryFromOriginHTTPS(t *testing.T) {
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Repository != "github.com/owner/repo" {
		t.Errorf("Repository: got %q want %q", meta.Repository, "github.com/owner/repo")
	}
}

func TestCollectGitMetadataRepositoryFromOriginHTTPSWithCredentials(t *testing.T) {
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://user:token@github.com/owner/repo.git")

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Repository != "github.com/owner/repo" {
		t.Errorf("Repository: got %q want %q (credentials must be stripped)", meta.Repository, "github.com/owner/repo")
	}
}

func TestCollectGitMetadataRepositoryFromOriginSCPStyleSSH(t *testing.T) {
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "git@github.com:owner/repo.git")

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Repository != "github.com/owner/repo" {
		t.Errorf("Repository: got %q want %q", meta.Repository, "github.com/owner/repo")
	}
}

func TestCollectGitMetadataRepositoryFromOriginURLStyleSSH(t *testing.T) {
	dir := initRepo(t)
	runGit(t, dir, "remote", "add", "origin", "ssh://git@github.com/owner/repo.git")

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Repository != "github.com/owner/repo" {
		t.Errorf("Repository: got %q want %q", meta.Repository, "github.com/owner/repo")
	}
}

func TestCollectGitMetadataRepositoryFallsBackToTopLevelDirName(t *testing.T) {
	dir := initRepo(t)

	meta, err := CollectGitMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Repository != filepath.Base(dir) {
		t.Errorf("Repository: got %q want %q", meta.Repository, filepath.Base(dir))
	}
}

func TestCollectGitMetadataMissingRepository(t *testing.T) {
	dir := t.TempDir()

	if _, err := CollectGitMetadata(context.Background(), dir); err == nil {
		t.Fatal("expected an error for a non-git directory")
	}
}

func TestCollectGitMetadataEmptyRepoDirUsesCurrentDirectory(t *testing.T) {
	dir := initRepo(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore Chdir: %v", err)
		}
	}()

	meta, err := CollectGitMetadata(context.Background(), "")
	if err != nil {
		t.Fatalf("CollectGitMetadata: %v", err)
	}
	if meta.Branch != "main" {
		t.Errorf("Branch: got %q want %q", meta.Branch, "main")
	}
}

func TestGitMetadataTagsOrderAndValues(t *testing.T) {
	meta := GitMetadata{
		Commit:     "abc123",
		Branch:     "main",
		Dirty:      true,
		Author:     "Jane Doe <jane@example.com>",
		Repository: "github.com/owner/repo",
	}
	tags := meta.Tags()
	wantKeys := []string{GitTagCommit, GitTagBranch, GitTagDirty, GitTagCommitDate, GitTagAuthor, GitTagRepository}
	if len(tags) != len(wantKeys) {
		t.Fatalf("got %d tags want %d", len(tags), len(wantKeys))
	}
	for i, want := range wantKeys {
		if tags[i].Key != want {
			t.Errorf("tags[%d].Key: got %q want %q", i, tags[i].Key, want)
		}
	}
	if tags[2].Value != "true" {
		t.Errorf("git.dirty: got %q want %q", tags[2].Value, "true")
	}
}
