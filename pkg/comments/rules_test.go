package comments

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func newRulesDataset(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ds, err := OpenDataset(dir, Filters{Repos: []string{"o/r"}})
	if err != nil {
		t.Fatalf("OpenDataset: %v", err)
	}
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = ds.AppendPR(&PR{Repo: "o/r", Number: 1, Title: "t", URL: "u", State: "closed", CreatedAt: base, UpdatedAt: base})
	_ = ds.AppendPR(&PR{Repo: "o/r", Number: 2, Title: "t", URL: "u", State: "closed", CreatedAt: base, UpdatedAt: base})
	cs := []Comment{
		{ID: 1, Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: 1, Author: "a", Body: "please rename this variable", CreatedAt: base.Add(1 * time.Hour), ReviewState: "CHANGES_REQUESTED", URL: "u1"},
		{ID: 2, Type: CommentTypeReviewBody, Repo: "o/r", PRNumber: 1, Author: "b", Body: "Add unit test for this function please", CreatedAt: base.Add(2 * time.Hour), ReviewState: "COMMENTED", URL: "u2"},
		{ID: 3, Type: CommentTypeReviewComment, Repo: "o/r", PRNumber: 2, Author: "c", Body: "wrap this error with fmt.Errorf", CreatedAt: base.Add(3 * time.Hour), ReviewState: "CHANGES_REQUESTED", URL: "u3", Path: "main.go"},
		{ID: 4, Type: CommentTypeReviewComment, Repo: "o/r", PRNumber: 2, Author: "a", Body: "this needs a better name", CreatedAt: base.Add(4 * time.Hour), URL: "u4", Path: "main.go"},
		{ID: 5, Type: CommentTypeIssueComment, Repo: "o/r", PRNumber: 2, Author: "b", Body: "looks fine to me", CreatedAt: base.Add(5 * time.Hour), URL: "u5"},
	}
	for i := range cs {
		_ = ds.AppendComment(&cs[i])
	}
	if err := ds.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dir
}

func TestSuggestRulesDefaultTopics(t *testing.T) {
	dir := newRulesDataset(t)
	res, err := SuggestRules(dir, SuggestRulesOptions{MinCount: 1, MinReviewers: 1, Examples: 5})
	if err != nil {
		t.Fatalf("SuggestRules: %v", err)
	}
	got := map[string]RuleCandidate{}
	for _, c := range res.Candidates {
		got[c.Topic] = c
	}
	if _, ok := got["naming"]; !ok {
		t.Fatalf("expected naming topic, candidates=%v", got)
	}
	naming := got["naming"]
	if naming.Count != 2 {
		t.Errorf("naming count: got %d want 2", naming.Count)
	}
	if naming.DistinctReviewers != 1 {
		t.Errorf("naming reviewers: got %d want 1", naming.DistinctReviewers)
	}
	if naming.BlockingCount < 1 {
		t.Errorf("naming blocking: got %d want >=1", naming.BlockingCount)
	}
	if len(naming.Examples) == 0 {
		t.Error("expected naming examples")
	}
}

func TestSuggestRulesThresholds(t *testing.T) {
	dir := newRulesDataset(t)
	res, err := SuggestRules(dir, SuggestRulesOptions{MinCount: 100, MinReviewers: 100})
	if err != nil {
		t.Fatalf("SuggestRules: %v", err)
	}
	if len(res.Candidates) != 0 {
		t.Fatalf("expected no candidates with high thresholds, got %d", len(res.Candidates))
	}
}

func TestBuildReportMarkdown(t *testing.T) {
	dir := newRulesDataset(t)
	r, err := BuildReport(dir, ReportOptions{MinCount: 1, MinReviewers: 1, Examples: 2, StatsTop: 5})
	if err != nil {
		t.Fatalf("BuildReport: %v", err)
	}
	if r.Manifest == nil {
		t.Fatal("manifest missing in report")
	}
	if r.Stats["comment_type"] == nil || len(r.Stats["comment_type"].Rows) == 0 {
		t.Fatal("expected comment_type stats rows")
	}
	if r.Rules == nil || len(r.Rules.Candidates) == 0 {
		t.Fatal("expected rule candidates")
	}
	var buf bytes.Buffer
	if err := WriteReportMarkdown(&buf, r); err != nil {
		t.Fatalf("WriteReportMarkdown: %v", err)
	}
	md := buf.String()
	for _, want := range []string{"# Comments Report", "## Top Rule Candidates", "## Stats", "naming"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n%s", want, md)
		}
	}
}
