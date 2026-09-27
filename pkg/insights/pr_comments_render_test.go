package insights

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

func TestRenderPRCommentsEmpty(t *testing.T) {
	sr := render.NewStringRenderer(nil)

	if err := RenderPRComments(&sr.Renderer, nil); err != nil {
		t.Fatalf("RenderPRComments(empty): %v", err)
	}

	if got := sr.Stdout.String(); !strings.Contains(got, "no review feedback found") {
		t.Fatalf("empty output missing message:\n%s", got)
	}
}

func TestRenderPRCommentsText(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	comments := []*Comment{
		{
			ID:          10,
			Type:        CommentTypeReviewBody,
			Author:      "alice",
			CreatedAt:   created,
			ReviewState: "CHANGES_REQUESTED",
			URL:         "https://example.test/10",
			Body:        "please fix",
		},
		{
			// Outdated inline comment: Line is 0, so OriginalLine is used.
			ID:           11,
			Type:         CommentTypeReviewComment,
			Author:       "bob",
			CreatedAt:    created,
			Path:         "src/main.go",
			Line:         0,
			OriginalLine: 42,
			Outdated:     true,
			URL:          "https://example.test/11",
			Body:         "use range loop",
		},
	}

	sr := render.NewStringRenderer(nil)
	if err := RenderPRComments(&sr.Renderer, comments); err != nil {
		t.Fatalf("RenderPRComments(text): %v", err)
	}

	got := sr.Stdout.String()
	for _, want := range []string{
		"10 review_body",
		"author=alice",
		"review_state=CHANGES_REQUESTED",
		"11 review_comment src/main.go:42",
		"outdated=true",
		"body=use range loop",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}

	// review_body must not emit inline-only fields.
	if strings.Contains(got, "outdated=false") {
		t.Fatalf("review_body should not emit outdated field:\n%s", got)
	}
}

func TestRenderPRCommentsJSON(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	comments := []*Comment{
		{ID: 10, Type: CommentTypeIssueComment, Author: "alice", CreatedAt: created, Body: "looks good"},
	}

	sr := render.NewStringRenderer(cmdutil.NewJSONExporter())
	if err := RenderPRComments(&sr.Renderer, comments); err != nil {
		t.Fatalf("RenderPRComments(json): %v", err)
	}

	var decoded []*Comment
	if err := json.Unmarshal(sr.Stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(decoded) != 1 || decoded[0].ID != 10 || decoded[0].Type != CommentTypeIssueComment {
		t.Fatalf("unexpected decoded result: %+v", decoded)
	}
}
