package attestation

import (
	"context"
	"strings"
	"testing"

	"github.com/srz-zumix/go-gh-extension/pkg/httputil"
)

// TestUpdateAssetsRejectsOutputWithoutAssetURL verifies the exported API
// enforces the Output-requires-AssetURL invariant before any GitHub call, so
// the check triggers even with a nil client.
func TestUpdateAssetsRejectsOutputWithoutAssetURL(t *testing.T) {
	_, err := UpdateAssets(context.Background(), nil, UpdateAssetsOptions{
		Output: "out.png",
	})
	if err == nil || !strings.Contains(err.Error(), "output path is only valid") {
		t.Fatalf("expected output-requires-asset-url error, got %v", err)
	}
}

func TestResolveUploadName(t *testing.T) {
	tests := []struct {
		name            string
		filename        string
		meta            httputil.AssetMeta
		wantName        string
		wantContentType string
		wantOK          bool
	}{
		{
			name:            "extension already usable",
			filename:        "shot.png",
			meta:            httputil.AssetMeta{Size: 1024, ContentType: "image/png"},
			wantName:        "shot.png",
			wantContentType: "image/png",
			wantOK:          true,
		},
		{
			name:            "extension from redirect hint",
			filename:        "3a7a2a1c_avater-a",
			meta:            httputil.AssetMeta{Size: 1024, ExtHint: ".png"},
			wantName:        "3a7a2a1c_avater-a.png",
			wantContentType: "image/png",
			wantOK:          true,
		},
		{
			name:            "extension from content type",
			filename:        "3a7a2a1c_avater-a",
			meta:            httputil.AssetMeta{Size: 1024, ContentType: "image/png; charset=binary"},
			wantName:        "3a7a2a1c_avater-a.png",
			wantContentType: "image/png",
			wantOK:          true,
		},
		{
			name:     "unsupported type",
			filename: "notes",
			meta:     httputil.AssetMeta{Size: 1024, ContentType: "application/zip"},
			wantName: "notes",
			wantOK:   false,
		},
		{
			name:     "size over the limit",
			filename: "shot.png",
			meta:     httputil.AssetMeta{Size: 1 << 30, ContentType: "image/png"},
			wantName: "shot.png",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotContentType, ok := resolveUploadName(tt.filename, tt.meta)
			if ok != tt.wantOK {
				t.Fatalf("resolveUploadName() ok = %v, want %v", ok, tt.wantOK)
			}
			if gotName != tt.wantName {
				t.Errorf("resolveUploadName() name = %q, want %q", gotName, tt.wantName)
			}
			if gotContentType != tt.wantContentType {
				t.Errorf("resolveUploadName() contentType = %q, want %q", gotContentType, tt.wantContentType)
			}
		})
	}
}

func TestReplaceAssetURLs(t *testing.T) {
	const base = "https://github.com/user-attachments/assets/aaaa"
	const long = base + "-bbbb"

	tests := []struct {
		name         string
		text         string
		replacements map[string]string
		want         string
		wantChanged  bool
	}{
		{
			name:         "empty text",
			text:         "",
			replacements: map[string]string{base: "https://example.com/new"},
			want:         "",
			wantChanged:  false,
		},
		{
			name:         "no replacements",
			text:         "![demo](" + base + ")",
			replacements: nil,
			want:         "![demo](" + base + ")",
			wantChanged:  false,
		},
		{
			name:         "no match",
			text:         "no assets here",
			replacements: map[string]string{base: "https://example.com/new"},
			want:         "no assets here",
			wantChanged:  false,
		},
		{
			name:         "replaces every occurrence",
			text:         base + " and " + base,
			replacements: map[string]string{base: "https://example.com/new"},
			want:         "https://example.com/new and https://example.com/new",
			wantChanged:  true,
		},
		{
			name: "longest url replaced first",
			text: base + " " + long,
			replacements: map[string]string{
				base: "https://example.com/short",
				long: "https://example.com/long",
			},
			want:        "https://example.com/short https://example.com/long",
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := replaceAssetURLs(tt.text, tt.replacements)
			if got != tt.want {
				t.Errorf("replaceAssetURLs() text = %q, want %q", got, tt.want)
			}
			if changed != tt.wantChanged {
				t.Errorf("replaceAssetURLs() changed = %v, want %v", changed, tt.wantChanged)
			}
		})
	}
}

func TestFilterScannedAssets(t *testing.T) {
	scanned := []scannedAsset{
		{url: "https://github.com/user-attachments/assets/aaaa", filename: "a.png", location: LocationBody},
		{url: "https://github.com/user-attachments/assets/bbbb", filename: "b.png", location: LocationIssueComment, locationID: 42},
	}

	t.Run("match", func(t *testing.T) {
		got, err := filterScannedAssets(scanned, scanned[1].url)
		if err != nil {
			t.Fatalf("filterScannedAssets() error = %v", err)
		}
		if len(got) != 1 || got[0].url != scanned[1].url {
			t.Errorf("filterScannedAssets() = %+v, want only %q", got, scanned[1].url)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, err := filterScannedAssets(scanned, "https://github.com/user-attachments/assets/cccc")
		if err == nil {
			t.Fatal("filterScannedAssets() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "is not referenced by the target") {
			t.Errorf("filterScannedAssets() error = %v, want it to mention the asset is not referenced", err)
		}
	})
}
