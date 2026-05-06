package comments

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{name: "github token", in: "token=ghp_" + strings.Repeat("a", 40), want: "token=[REDACTED]", changed: true},
		{name: "aws key", in: "use AKIA" + strings.Repeat("A", 16) + " here", want: "use [REDACTED] here", changed: true},
		{name: "no match", in: "this is fine", want: "this is fine", changed: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := Redact(c.in)
			if got != c.want {
				t.Errorf("body: got %q want %q", got, c.want)
			}
			if changed != c.changed {
				t.Errorf("changed: got %v want %v", changed, c.changed)
			}
		})
	}
}

func TestDatasetAppendValidateStats(t *testing.T) {
	dir := t.TempDir()
	ds, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	now := time.Now().UTC()
	if err := ds.AppendPR(&PR{Repo: "o/r", Number: 1, Title: "t", URL: "u", State: "closed", Merged: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AppendPR: %v", err)
	}
	if err := ds.AppendComment(&Comment{ID: 10, Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: 1, Author: "alice", Body: "please fix", CreatedAt: now, ReviewState: "CHANGES_REQUESTED", URL: "https://example/1"}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}
	if err := ds.AppendComment(&Comment{ID: 11, Type: CommentTypeReviewComment, Repo: "o/r", PRNumber: 1, Author: "bob", Body: "use range loop", CreatedAt: now, Path: "src/main.go"}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}
	ds.MarkPRDone("o/r", 1, now)
	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	report, err := Validate(dir)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Comments != 2 || report.PRs != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("unexpected issues: %v", report.Issues)
	}

	stats, err := Stats(dir, StatsOptions{GroupBy: "comment_type"})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if len(stats.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(stats.Rows))
	}

	// Re-open the dataset to confirm checkpoint persists.
	ds2, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !ds2.IsPRDone("o/r", 1) {
		t.Fatalf("expected PR 1 to be marked done after reopen")
	}
	if err := ds2.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
	// Sanity: manifest on disk
	if _, err := LoadManifest(dir); err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if filepath.Base(dir) == "" {
		t.Fatal("temp dir base unexpectedly empty")
	}
}
