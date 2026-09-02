package attestation

import (
	"strings"
	"testing"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

func TestRenderPRAssets_Text(t *testing.T) {
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
		{
			Filename:   "broken.png",
			Location:   LocationReviewComment,
			LocationID: 7,
			Error:      "download failed",
		},
	}

	if err := RenderPRAssets(&sr.Renderer, assets); err != nil {
		t.Fatalf("RenderPRAssets: %v", err)
	}

	got := sr.Stdout.String()
	for _, want := range []string{
		"attested.png (body)", "git.commit=abcdef1234567890", "git.branch=main",
		"plain.png (issue_comment#42)", "no attestation found",
		"broken.png (review_comment#7)", "error=download failed",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("text output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderPRAssets_JSON(t *testing.T) {
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
