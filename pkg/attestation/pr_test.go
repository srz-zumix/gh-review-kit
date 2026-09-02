package attestation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

func TestScanAssetURLs(t *testing.T) {
	patterns := gh.BuildAssetURLPatterns("github.com")

	body := "See ![screenshot.png](https://github.com/user-attachments/assets/11111111-1111-1111-1111-111111111111) for details."
	comment := "Also check [demo.mp4](https://github.com/user-attachments/assets/22222222-2222-2222-2222-222222222222)."
	// Duplicate URL across body and comment must be deduplicated, keeping the
	// first (body) occurrence.
	dup := "https://github.com/user-attachments/assets/11111111-1111-1111-1111-111111111111 again."

	texts := []scannedText{
		{text: body, location: LocationBody},
		{text: comment, location: LocationIssueComment, locationID: 42},
		{text: dup, location: LocationReviewComment, locationID: 99},
	}

	got := scanAssetURLs(patterns, texts)
	if len(got) != 2 {
		t.Fatalf("scanAssetURLs returned %d assets, want 2: %+v", len(got), got)
	}

	if got[0].url != "https://github.com/user-attachments/assets/11111111-1111-1111-1111-111111111111" {
		t.Errorf("got[0].url = %q", got[0].url)
	}
	if got[0].filename != "screenshot.png" {
		t.Errorf("got[0].filename = %q, want screenshot.png", got[0].filename)
	}
	if got[0].location != LocationBody {
		t.Errorf("got[0].location = %q, want body (first occurrence wins)", got[0].location)
	}

	if got[1].filename != "demo.mp4" {
		t.Errorf("got[1].filename = %q, want demo.mp4", got[1].filename)
	}
	if got[1].location != LocationIssueComment || got[1].locationID != 42 {
		t.Errorf("got[1] location/locationID = %q/%d, want issue_comment/42", got[1].location, got[1].locationID)
	}
}

func TestScanAssetURLsTrimsTrailingPunctuation(t *testing.T) {
	patterns := []*regexp.Regexp{regexp.MustCompile(`https://example\.com/asset/[^\s)<>"]+`)}
	texts := []scannedText{{text: "(see https://example.com/asset/1.png), thanks.", location: LocationBody}}

	got := scanAssetURLs(patterns, texts)
	if len(got) != 1 {
		t.Fatalf("scanAssetURLs returned %d assets, want 1: %+v", len(got), got)
	}
	if got[0].url != "https://example.com/asset/1.png" {
		t.Errorf("got[0].url = %q, want trailing punctuation stripped", got[0].url)
	}
}

func TestFetchPRAssetsWithAndWithoutAttestation(t *testing.T) {
	attested, err := pngEmbedTags(samplePNG(t), []Tag{
		{Key: GitTagCommit, Value: "abc123"},
		{Key: GitTagBranch, Value: "main"},
		{Key: CommentTag, Value: "hello"},
	})
	if err != nil {
		t.Fatalf("pngEmbedTags: %v", err)
	}
	plain := samplePNG(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/attested.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(attested)
	})
	mux.HandleFunc("/plain.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(plain)
	})
	mux.HandleFunc("/missing.png", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	scanned := []scannedAsset{
		{url: server.URL + "/attested.png", filename: "attested.png", location: LocationBody},
		{url: server.URL + "/plain.png", filename: "plain.png", location: LocationIssueComment, locationID: 1},
		{url: server.URL + "/missing.png", filename: "missing.png", location: LocationReviewComment, locationID: 2},
	}

	assets, err := fetchPRAssets(context.Background(), server.Client(), "github.com", 7, "https://github.com/o/r/pull/7", scanned, 0)
	if err != nil {
		t.Fatalf("fetchPRAssets: %v", err)
	}
	if len(assets) != 3 {
		t.Fatalf("fetchPRAssets returned %d assets, want 3", len(assets))
	}

	attestedAsset := assets[0]
	if !attestedAsset.Attested {
		t.Error("attested asset: Attested = false, want true")
	}
	if attestedAsset.Error != "" {
		t.Errorf("attested asset: Error = %q, want empty", attestedAsset.Error)
	}
	if len(attestedAsset.Tags) == 0 {
		t.Error("attested asset: Tags is empty, want git metadata tags")
	}

	plainAsset := assets[1]
	if plainAsset.Attested {
		t.Error("plain asset: Attested = true, want false")
	}
	if plainAsset.Error != "" {
		t.Errorf("plain asset: Error = %q, want empty (no attestation is not an error)", plainAsset.Error)
	}
	if len(plainAsset.Tags) != 0 {
		t.Errorf("plain asset: Tags = %v, want empty", plainAsset.Tags)
	}

	missingAsset := assets[2]
	if missingAsset.Attested {
		t.Error("missing asset: Attested = true, want false")
	}
	if missingAsset.Error == "" {
		t.Error("missing asset: Error is empty, want a download error recorded")
	}

	for _, a := range assets {
		if a.PRNumber != 7 {
			t.Errorf("PRNumber = %d, want 7", a.PRNumber)
		}
		if a.PRURL != "https://github.com/o/r/pull/7" {
			t.Errorf("PRURL = %q, want pull request URL", a.PRURL)
		}
	}
}

func TestFetchPRAssetsSkipsOversizedAsset(t *testing.T) {
	var downloaded bool
	mux := http.NewServeMux()
	mux.HandleFunc("/big.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		if r.Method == http.MethodHead {
			return
		}
		downloaded = true
		_, _ = w.Write(make([]byte, 1000))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	scanned := []scannedAsset{
		{url: server.URL + "/big.png", filename: "big.png", location: LocationBody},
	}

	assets, err := fetchPRAssets(context.Background(), server.Client(), "github.com", 7, "", scanned, 100)
	if err != nil {
		t.Fatalf("fetchPRAssets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("fetchPRAssets returned %d assets, want 1", len(assets))
	}
	if downloaded {
		t.Error("oversized asset was downloaded, want it skipped before download")
	}
	a := assets[0]
	if a.Attested {
		t.Error("oversized asset: Attested = true, want false")
	}
	if !strings.Contains(a.Error, "skipped") {
		t.Errorf("oversized asset: Error = %q, want a skip message", a.Error)
	}
	if a.FileSize != 1000 {
		t.Errorf("oversized asset: FileSize = %d, want 1000", a.FileSize)
	}
}

func TestFetchPRAssetsEmpty(t *testing.T) {
	assets, err := fetchPRAssets(context.Background(), http.DefaultClient, "github.com", 1, "", nil, 0)
	if err != nil {
		t.Fatalf("fetchPRAssets: %v", err)
	}
	if assets != nil {
		t.Errorf("fetchPRAssets(nil) = %v, want nil", assets)
	}
}
