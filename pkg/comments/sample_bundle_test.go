package comments

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestDataset(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ds, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_ = ds.AppendPR(&PR{Repo: "o/r", Number: i + 1, Title: "t", URL: "u", State: "closed", CreatedAt: base, UpdatedAt: base})
	}
	// 6 review comments across 3 authors, mixed states/paths
	cases := []Comment{
		{ID: 1, Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: 1, Author: "a", Body: "block this", CreatedAt: base.Add(1 * time.Hour), ReviewState: "CHANGES_REQUESTED", URL: "u1"},
		{ID: 2, Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: 1, Author: "b", Body: "lgtm", CreatedAt: base.Add(2 * time.Hour), ReviewState: "APPROVED", URL: "u2"},
		{ID: 3, Type: CommentTypeReviewComment, Repo: "o/r", PRNumber: 2, Author: "a", Body: "rename var", CreatedAt: base.Add(3 * time.Hour), Path: "src/main.go", URL: "u3"},
		{ID: 4, Type: CommentTypeReviewComment, Repo: "o/r", PRNumber: 2, Author: "c", Body: "add test", CreatedAt: base.Add(4 * time.Hour), Path: "pkg/util.go", URL: "u4"},
		{ID: 5, Type: CommentTypeIssueComment, Repo: "o/r", PRNumber: 3, Author: "b", Body: "ping", CreatedAt: base.Add(5 * time.Hour), URL: "u5"},
		{ID: 6, Type: CommentTypeIssueComment, Repo: "o/r", PRNumber: 3, Author: "a", Body: "thanks", CreatedAt: base.Add(6 * time.Hour), URL: "u6"},
	}
	for i := range cases {
		_ = ds.AppendComment(&cases[i])
	}
	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

func TestSampleRecent(t *testing.T) {
	dir := newTestDataset(t)
	got, err := Sample(dir, SampleOptions{PerGroup: 2, Strategy: SampleStrategyRecent})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].ID != 6 || got[1].ID != 5 {
		t.Fatalf("expected newest first, got ids=%d,%d", got[0].ID, got[1].ID)
	}
}

func TestSampleBlockingPerGroup(t *testing.T) {
	dir := newTestDataset(t)
	got, err := Sample(dir, SampleOptions{
		GroupBy:  "comment_type",
		PerGroup: 5,
		Strategy: SampleStrategyBlocking,
	})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("expected only blocking record id=1, got %+v", got)
	}
}

func TestSampleDiverseAuthors(t *testing.T) {
	dir := newTestDataset(t)
	got, err := Sample(dir, SampleOptions{PerGroup: 3, Strategy: SampleStrategyDiverseAuthors})
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c.Author] {
			t.Fatalf("duplicate author %q in diverse-authors output", c.Author)
		}
		seen[c.Author] = true
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 distinct authors, got %d", len(got))
	}
}

func TestBundleByGroup(t *testing.T) {
	dir := newTestDataset(t)
	out := t.TempDir()
	manifest, err := Bundle(dir, BundleOptions{
		OutputDir:  out,
		GroupBy:    "comment_type",
		MaxRecords: 2,
	})
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	if len(manifest.Bundles) == 0 {
		t.Fatal("expected bundles to be produced")
	}
	totalRecords := 0
	for _, b := range manifest.Bundles {
		totalRecords += b.Records
		if b.Records > 2 {
			t.Errorf("bundle %s exceeded MaxRecords: %d", b.File, b.Records)
		}
		path := filepath.Join(out, b.File)
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		s := bufio.NewScanner(f)
		lines := 0
		for s.Scan() {
			if strings.TrimSpace(s.Text()) != "" {
				lines++
			}
		}
		_ = f.Close()
		if lines != b.Records {
			t.Errorf("bundle %s line count=%d want %d", b.File, lines, b.Records)
		}
	}
	if totalRecords != 6 {
		t.Errorf("total records=%d want 6", totalRecords)
	}
}

func TestBundleRequiresCap(t *testing.T) {
	dir := newTestDataset(t)
	out := t.TempDir()
	if _, err := Bundle(dir, BundleOptions{OutputDir: out}); err == nil {
		t.Fatal("expected error when neither --max-records nor --max-bytes is set")
	}
}

func TestSampleGroupFnNormalizesEmptyKeys(t *testing.T) {
	// No --group-by keeps the single global group ("" key).
	fn, err := sampleGroupFn("")
	if err != nil {
		t.Fatalf("sampleGroupFn(\"\"): %v", err)
	}
	if got := fn(&Comment{}, nil); len(got) != 1 || got[0] != "" {
		t.Fatalf("empty group: got %v want [\"\"]", got)
	}

	// Empty author is bucketed as "(none)".
	fn, err = sampleGroupFn("author")
	if err != nil {
		t.Fatalf("sampleGroupFn(author): %v", err)
	}
	if got := fn(&Comment{Author: ""}, nil); len(got) != 1 || got[0] != "(none)" {
		t.Fatalf("empty author: got %v want [\"(none)\"]", got)
	}
	if got := fn(&Comment{Author: "alice"}, nil); len(got) != 1 || got[0] != "alice" {
		t.Fatalf("author: got %v want [\"alice\"]", got)
	}

	// path_prefix still yields no keys for comments without a path.
	fn, err = sampleGroupFn("path_prefix")
	if err != nil {
		t.Fatalf("sampleGroupFn(path_prefix): %v", err)
	}
	if got := fn(&Comment{Path: ""}, nil); got != nil {
		t.Fatalf("empty path: got %v want nil", got)
	}
}

func TestBundleGroupByAuthorBucketsEmptyAuthor(t *testing.T) {
	dir := t.TempDir()
	ds, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = ds.AppendPR(&PR{Repo: "o/r", Number: 1, CreatedAt: base, UpdatedAt: base})
	_ = ds.AppendComment(&Comment{ID: 1, Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: 1, Author: "alice", Body: "a", CreatedAt: base, URL: "u1"})
	_ = ds.AppendComment(&Comment{ID: 2, Type: CommentTypeIssueComment, Repo: "o/r", PRNumber: 1, Author: "", Body: "b", CreatedAt: base, URL: "u2"})
	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out := t.TempDir()
	manifest, err := Bundle(dir, BundleOptions{OutputDir: out, GroupBy: "author", MaxRecords: 10})
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	var noneEntry *BundleEntry
	for i := range manifest.Bundles {
		if manifest.Bundles[i].Group == "(none)" {
			noneEntry = &manifest.Bundles[i]
		}
	}
	if noneEntry == nil {
		t.Fatalf("expected a bundle with Group=(none), got %+v", manifest.Bundles)
	}
	if noneEntry.File == "bundle-0001.jsonl" {
		t.Fatalf("empty-author bundle should not use the generic single-stream name, got %s", noneEntry.File)
	}
}
