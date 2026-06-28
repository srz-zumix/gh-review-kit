package comments

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestStatsRendererText(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewStatsRenderer(&buf)
	result := &StatsResult{
		Dataset: "dataset",
		GroupBy: "comment_type",
		Rows: []StatRow{
			{Key: "review_comment", Count: 3, Reviewers: 2, Repos: 1, Blocking: 1, ExampleURL: "https://example.test/1"},
		},
	}

	if err := renderer.Render(result, "text"); err != nil {
		t.Fatalf("Render(text): %v", err)
	}

	got := buf.String()
	for _, want := range []string{"KEY", "review_comment", "3", "https://example.test/1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}
}

func TestStatsRendererJSON(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewStatsRenderer(&buf)
	result := &StatsResult{
		Dataset: "dataset",
		GroupBy: "comment_type",
		Rows: []StatRow{{Key: "review_comment", Count: 3}},
	}

	if err := renderer.Render(result, "json"); err != nil {
		t.Fatalf("Render(json): %v", err)
	}

	var decoded StatsResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Dataset != result.Dataset || decoded.GroupBy != result.GroupBy || len(decoded.Rows) != 1 {
		t.Fatalf("unexpected decoded result: %+v", decoded)
	}
}