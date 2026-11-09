package copilot

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

func TestParseSourceFilters(t *testing.T) {
	got, err := ParseSourceFilters([]string{"skill", "MCP:github", "custom-instructions:.github/copilot-instructions.md", "None"})
	if err != nil {
		t.Fatal(err)
	}
	want := []SourceFilter{
		{Kind: gh.CopilotCodeReviewSourceKindSkill},
		{Kind: gh.CopilotCodeReviewSourceKindMCP, Name: "github"},
		{Kind: gh.CopilotCodeReviewSourceKindCustomInstructions, Name: ".github/copilot-instructions.md"},
		{None: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSourceFilters() = %+v, want %+v", got, want)
	}
	if _, err := ParseSourceFilters([]string{"tool:x"}); err == nil {
		t.Error("ParseSourceFilters(tool:x) error = nil, want an error")
	}
}

func TestFilterCommentsBySource(t *testing.T) {
	skill := &Comment{CommentID: 1, Sources: []gh.CopilotCodeReviewSource{{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "code-review", Path: ".github/skills/code-review/SKILL.md"}}}
	mcp := &Comment{CommentID: 2, Sources: []gh.CopilotCodeReviewSource{{Kind: gh.CopilotCodeReviewSourceKindMCP, Name: "github"}}}
	none := &Comment{CommentID: 3}
	comments := []*Comment{skill, mcp, none}

	tests := []struct {
		name    string
		filters []SourceFilter
		want    []*Comment
	}{
		{"no filter keeps all", nil, comments},
		{"kind", []SourceFilter{{Kind: gh.CopilotCodeReviewSourceKindSkill}}, []*Comment{skill}},
		{"name is case-insensitive", []SourceFilter{{Kind: gh.CopilotCodeReviewSourceKindMCP, Name: "GitHub"}}, []*Comment{mcp}},
		{"path matches", []SourceFilter{{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: ".github/skills/code-review/SKILL.md"}}, []*Comment{skill}},
		{"name mismatch", []SourceFilter{{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "other"}}, nil},
		{"none", []SourceFilter{{None: true}}, []*Comment{none}},
		{"any filter matches", []SourceFilter{{Kind: gh.CopilotCodeReviewSourceKindMCP}, {None: true}}, []*Comment{mcp, none}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterCommentsBySource(comments, tt.filters)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterCommentsBySource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatSource(t *testing.T) {
	tests := []struct {
		source gh.CopilotCodeReviewSource
		want   string
	}{
		{gh.CopilotCodeReviewSource{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "code-review", Path: ".github/skills/code-review/SKILL.md"}, "SKILL:code-review"},
		{gh.CopilotCodeReviewSource{Kind: gh.CopilotCodeReviewSourceKindCustomInstructions, Name: "custom instructions", Path: ".github/copilot-instructions.md"}, "CUSTOM_INSTRUCTIONS:.github/copilot-instructions.md"},
		{gh.CopilotCodeReviewSource{Kind: gh.CopilotCodeReviewSourceKindCustomInstructions, Name: "custom instructions"}, "CUSTOM_INSTRUCTIONS:custom instructions"},
	}
	for _, tt := range tests {
		if got := FormatSource(tt.source); got != tt.want {
			t.Errorf("FormatSource(%+v) = %q, want %q", tt.source, got, tt.want)
		}
	}
}

func TestSummarizeVerdictsBySource(t *testing.T) {
	skill := gh.CopilotCodeReviewSource{Kind: gh.CopilotCodeReviewSourceKindSkill, Name: "code-review"}
	mcp := gh.CopilotCodeReviewSource{Kind: gh.CopilotCodeReviewSourceKindMCP, Name: "github"}
	result := func(v Verdict, sources ...gh.CopilotCodeReviewSource) *EvaluationResult {
		res := &EvaluationResult{Comment: &Comment{Sources: sources}}
		if v != "" {
			res.Evaluation = &Evaluation{Verdict: v}
		}
		return res
	}
	results := []*EvaluationResult{
		result(VerdictInvalid, skill),
		result(VerdictInvalid, skill, mcp),
		result(VerdictValid, skill, skill),
		result(VerdictUnclear),
		result("", mcp),
		nil,
	}
	got := SummarizeVerdictsBySource(results)
	want := []SourceVerdictSummary{
		{Source: "SKILL:code-review", Total: 3, Valid: 1, Invalid: 2},
		{Source: "MCP:github", Total: 2, Invalid: 1, Failed: 1},
		{Source: SourceFilterNone, Total: 1, Unclear: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SummarizeVerdictsBySource() = %+v, want %+v", got, want)
	}
	if rate := got[0].ValidRate(); rate < 0.333 || rate > 0.334 {
		t.Errorf("ValidRate() = %f, want 1/3", rate)
	}
	if rate := got[1].ValidRate(); rate != 0 {
		t.Errorf("ValidRate() = %f, want 0", rate)
	}
	if hasAttributedSource(got[2:]) {
		t.Error("hasAttributedSource(none only) = true, want false")
	}
}

func TestListCommentsWithAttribution(t *testing.T) {
	restCalls := 0
	g := newReplyTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/graphql":
			_, _ = fmt.Fprintf(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
				{"id":"THREAD_1","isResolved":false,"isOutdated":false,"comments":{"nodes":[
					{"databaseId":11,"body":"skill","author":{"login":%q,"__typename":"Bot"}}
				],"pageInfo":{"hasNextPage":false,"endCursor":null}}},
				{"id":"THREAD_2","isResolved":false,"isOutdated":false,"comments":{"nodes":[
					{"databaseId":22,"body":"plain","author":{"login":%q,"__typename":"Bot"}}
				],"pageInfo":{"hasNextPage":false,"endCursor":null}}}
			],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`, DefaultAuthor, DefaultAuthor)
		case "/repos/example-org/example-repo/pulls/7/comments":
			restCalls++
			_, _ = w.Write([]byte(`[{"id":11,"is_copilot_code_review":true,"copilot_code_review_sources":[{"kind":"SKILL","name":"code-review"}]},{"id":22,"is_copilot_code_review":true}]`))
		default:
			t.Errorf("unexpected API path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}, false)

	comments, err := ListComments(context.Background(), g, replyTestRepo, 7, ListOptions{WithAttribution: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 || len(comments[0].Sources) != 1 || comments[0].Sources[0].Name != "code-review" {
		t.Fatalf("comments = %+v, want sources attached to comment 11", comments)
	}
	if comments[1].IsCopilotCodeReview == nil || !*comments[1].IsCopilotCodeReview || len(comments[1].Sources) != 0 {
		t.Errorf("comment 22 attribution = %+v", comments[1])
	}

	filtered, err := ListComments(context.Background(), g, replyTestRepo, 7, ListOptions{Sources: []SourceFilter{{None: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].CommentID != 22 {
		t.Errorf("filtered = %+v, want only comment 22", filtered)
	}

	restCalls = 0
	if _, err := ListComments(context.Background(), g, replyTestRepo, 7, ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if restCalls != 0 {
		t.Errorf("REST calls without attribution = %d, want 0", restCalls)
	}
}
