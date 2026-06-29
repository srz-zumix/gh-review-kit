package comments

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

func TestRenderStatsText(t *testing.T) {
	sr := render.NewStringRenderer(nil)
	result := &StatsResult{
		Dataset: "dataset",
		GroupBy: "comment_type",
		Rows: []StatRow{
			{Key: "review_comment", Count: 3, Reviewers: 2, Repos: 1, Blocking: 1, ExampleURL: "https://example.test/1"},
		},
	}

	if err := RenderStats(&sr.Renderer, result); err != nil {
		t.Fatalf("RenderStats(text): %v", err)
	}

	got := sr.Stdout.String()
	for _, want := range []string{"KEY", "review_comment", "3", "https://example.test/1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderStatsJSON(t *testing.T) {
	sr := render.NewStringRenderer(cmdutil.NewJSONExporter())
	result := &StatsResult{
		Dataset: "dataset",
		GroupBy: "comment_type",
		Rows:    []StatRow{{Key: "review_comment", Count: 3}},
	}

	if err := RenderStats(&sr.Renderer, result); err != nil {
		t.Fatalf("RenderStats(json): %v", err)
	}

	var decoded StatsResult
	if err := json.Unmarshal(sr.Stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if decoded.Dataset != result.Dataset || decoded.GroupBy != result.GroupBy || len(decoded.Rows) != 1 {
		t.Fatalf("unexpected decoded result: %+v", decoded)
	}
}
