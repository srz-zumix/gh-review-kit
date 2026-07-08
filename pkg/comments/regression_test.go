package comments

import (
	"testing"
	"time"
)

func TestStatsAppliesFilters(t *testing.T) {
	dir := newRulesDataset(t)
	// Without filters, comment_type should have 3 keys.
	all, err := Stats(dir, StatsOptions{GroupBy: "comment_type"})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if len(all.Rows) != 3 {
		t.Fatalf("expected 3 comment_type rows, got %d", len(all.Rows))
	}
	// Restrict to CHANGES_REQUESTED only; dataset has 2 such records.
	filtered, err := Stats(dir, StatsOptions{
		GroupBy: "comment_type",
		Filters: SampleFilters{ReviewStates: []string{"CHANGES_REQUESTED"}},
	})
	if err != nil {
		t.Fatalf("Stats filtered: %v", err)
	}
	total := 0
	for _, r := range filtered.Rows {
		total += r.Count
	}
	if total != 2 {
		t.Fatalf("expected 2 records after CHANGES_REQUESTED filter, got %d", total)
	}
}

func TestSuggestRulesKeepsMostRecentExamples(t *testing.T) {
	dir := t.TempDir()
	ds, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := ds.AppendPR(&PR{Repo: "o/r", Number: 1, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("AppendPR: %v", err)
	}
	// Five "naming" matches in ascending CreatedAt; only the newest 2 must
	// survive when Examples=2 even though older records appear first on disk.
	for i := 0; i < 5; i++ {
		c := &Comment{
			ID:        int64(100 + i),
			Type:      CommentTypeReviewBody,
			Repo:      "o/r",
			PRNumber:  1,
			Author:    "a",
			Body:      "please rename this variable",
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
			URL:       "u",
		}
		if err := ds.AppendComment(c); err != nil {
			t.Fatalf("AppendComment: %v", err)
		}
	}
	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	res, err := SuggestRules(dir, SuggestRulesOptions{MinCount: 1, MinReviewers: 1, Examples: 2})
	if err != nil {
		t.Fatalf("SuggestRules: %v", err)
	}
	var naming *RuleCandidate
	for i := range res.Candidates {
		if res.Candidates[i].Topic == "naming" {
			naming = &res.Candidates[i]
			break
		}
	}
	if naming == nil {
		t.Fatal("naming candidate missing")
	}
	if len(naming.Examples) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(naming.Examples))
	}
	// Examples are sorted descending by CreatedAt; newest two are i=4 and i=3.
	want0 := base.Add(4 * time.Hour)
	want1 := base.Add(3 * time.Hour)
	if !naming.Examples[0].CreatedAt.Equal(want0) || !naming.Examples[1].CreatedAt.Equal(want1) {
		t.Fatalf("expected newest examples %v,%v got %v,%v",
			want0, want1, naming.Examples[0].CreatedAt, naming.Examples[1].CreatedAt)
	}
}
