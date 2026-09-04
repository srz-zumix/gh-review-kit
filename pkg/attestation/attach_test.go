package attestation

import (
	"strings"
	"testing"
)

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
