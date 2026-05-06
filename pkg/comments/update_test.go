package comments

import (
	"testing"
	"time"
)

func TestPurgePRsRewritesFilesAndCheckpoint(t *testing.T) {
	dir := t.TempDir()
	ds, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		if err := ds.AppendPR(&PR{Repo: "o/r", Number: i, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatalf("AppendPR: %v", err)
		}
		c := &Comment{ID: int64(i * 10), Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: i, Author: "a", Body: "b", CreatedAt: base, URL: "u"}
		if err := ds.AppendComment(c); err != nil {
			t.Fatalf("AppendComment: %v", err)
		}
		ds.MarkPRDone("o/r", i, base)
	}
	if err := ds.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := ds.Manifest().Counts.PRs; got != 3 {
		t.Fatalf("manifest PRs: got %d want 3", got)
	}

	prsRemoved, commentsRemoved, err := ds.PurgePRs("o/r", map[int]struct{}{1: {}, 3: {}})
	if err != nil {
		t.Fatalf("PurgePRs: %v", err)
	}
	if prsRemoved != 2 {
		t.Fatalf("prsRemoved: got %d want 2", prsRemoved)
	}
	if commentsRemoved.ReviewBodies != 2 {
		t.Fatalf("commentsRemoved.ReviewBodies: got %d want 2", commentsRemoved.ReviewBodies)
	}
	if got := ds.Manifest().Counts.PRs; got != 1 {
		t.Fatalf("manifest PRs after purge: got %d want 1", got)
	}
	if got := ds.Manifest().Counts.ReviewBodies; got != 1 {
		t.Fatalf("manifest ReviewBodies after purge: got %d want 1", got)
	}
	if ds.IsPRDone("o/r", 1) || ds.IsPRDone("o/r", 3) {
		t.Fatal("expected purged PRs to be marked not-done")
	}
	if !ds.IsPRDone("o/r", 2) {
		t.Fatal("expected non-purged PR 2 to remain done")
	}

	// Append another record after purge to verify writers were reopened.
	if err := ds.AppendPR(&PR{Repo: "o/r", Number: 1, CreatedAt: base, UpdatedAt: base.Add(time.Hour)}); err != nil {
		t.Fatalf("AppendPR after purge: %v", err)
	}
	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	report, err := Validate(dir)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.PRs != 2 || report.Comments != 1 {
		t.Fatalf("post-purge counts: PRs=%d Comments=%d want 2/1", report.PRs, report.Comments)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("unexpected issues after purge: %v", report.Issues)
	}
}

func TestCheckpointTracksPRUpdatedAt(t *testing.T) {
	cp := &Checkpoint{}
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(2 * time.Hour)
	cp.markDone("o/r", 7, t1)
	if got := cp.prUpdatedAt("o/r", 7); !got.Equal(t1) {
		t.Fatalf("got %v want %v", got, t1)
	}
	cp.markDone("o/r", 7, t2)
	if got := cp.prUpdatedAt("o/r", 7); !got.Equal(t2) {
		t.Fatalf("got %v want %v after advance", got, t2)
	}
	cp.removePRs("o/r", map[int]struct{}{7: {}})
	if cp.isDone("o/r", 7) {
		t.Fatal("expected PR 7 removed from checkpoint")
	}
	if got := cp.prUpdatedAt("o/r", 7); !got.IsZero() {
		t.Fatalf("expected zero updated_at after removal, got %v", got)
	}
}
