package attestation

import (
	"strings"
	"testing"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

func TestRenderTagsTableText(t *testing.T) {
	sr := render.NewStringRenderer(nil)
	tags := []Tag{
		{Key: GitTagCommit, Value: "abcdef1234567890"},
		{Key: GitTagBranch, Value: "main"},
		{Key: CommentTag, Value: "hello"},
	}

	if err := RenderTagsTable(&sr.Renderer, "video.mp4", tags); err != nil {
		t.Fatalf("RenderTagsTable: %v", err)
	}

	got := sr.Stdout.String()
	for _, want := range []string{"FILENAME", "video.mp4", "abcdef12", "main", "hello"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "abcdef1234567890") {
		t.Fatalf("table output should show a shortened commit SHA:\n%s", got)
	}
}

func TestRenderTagsTableJSON(t *testing.T) {
	sr := render.NewStringRenderer(cmdutil.NewJSONExporter())
	tags := []Tag{{Key: GitTagCommit, Value: "abc123"}}

	if err := RenderTagsTable(&sr.Renderer, "video.mp4", tags); err != nil {
		t.Fatalf("RenderTagsTable(json): %v", err)
	}

	got := sr.Stdout.String()
	if !strings.Contains(got, "git.commit") || !strings.Contains(got, "abc123") {
		t.Fatalf("JSON output missing tag data:\n%s", got)
	}
}

func TestRenderPRAssetsText(t *testing.T) {
	sr := render.NewStringRenderer(nil)
	assets := []*PRAsset{
		{
			Filename: "attested.png",
			Location: LocationBody,
			Tags: []Tag{
				{Key: GitTagCommit, Value: "abcdef1234567890"},
				{Key: GitTagBranch, Value: "main"},
			},
			Attested: true,
		},
		{
			Filename:   "plain.png",
			Location:   LocationIssueComment,
			LocationID: 42,
			Attested:   false,
		},
	}

	if err := RenderPRAssets(&sr.Renderer, assets); err != nil {
		t.Fatalf("RenderPRAssets: %v", err)
	}

	got := sr.Stdout.String()
	for _, want := range []string{"FILENAME", "attested.png", "abcdef12", "main", "plain.png", "issue_comment#42"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderPRAssetsJSON(t *testing.T) {
	sr := render.NewStringRenderer(cmdutil.NewJSONExporter())
	assets := []*PRAsset{{Filename: "attested.png", Location: LocationBody, Attested: true}}

	if err := RenderPRAssets(&sr.Renderer, assets); err != nil {
		t.Fatalf("RenderPRAssets(json): %v", err)
	}

	got := sr.Stdout.String()
	if !strings.Contains(got, "attested.png") {
		t.Fatalf("JSON output missing asset data:\n%s", got)
	}
}
