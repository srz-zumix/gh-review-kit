package attestation

import (
	"strings"
	"testing"
)

func TestClassifyAsset(t *testing.T) {
	// http(s) URLs are detected and their scheme is canonicalized to lower
	// case (net/http rejects an upper-case scheme).
	cases := []struct {
		in   string
		want string
	}{
		{"https://github.com/o/r/assets/1", "https://github.com/o/r/assets/1"},
		{"HTTPS://github.com/o/r/assets/1", "https://github.com/o/r/assets/1"},
		{"http://example.com/x.png", "http://example.com/x.png"},
	}
	for _, c := range cases {
		got, ok, err := classifyAsset(c.in)
		if err != nil {
			t.Fatalf("classifyAsset(%q): %v", c.in, err)
		}
		if !ok {
			t.Fatalf("classifyAsset(%q) ok = false, want true", c.in)
		}
		if got != c.want {
			t.Fatalf("classifyAsset(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// Local file paths and non-http schemes are not asset URLs and are not errors.
	for _, in := range []string{"output.mp4", "./dir/a.png", "/abs/path.png", "ftp://host/x", "C:/x.png"} {
		got, ok, err := classifyAsset(in)
		if err != nil {
			t.Fatalf("classifyAsset(%q): unexpected error %v", in, err)
		}
		if ok {
			t.Fatalf("classifyAsset(%q) = %q, ok = true, want false", in, got)
		}
	}

	// Malformed http(s) URLs surface an error rather than being misread as a
	// local file path.
	for _, in := range []string{"https://", "https://github.com/%zz"} {
		if _, ok, err := classifyAsset(in); err == nil {
			t.Fatalf("classifyAsset(%q): expected error, got ok=%v", in, ok)
		}
	}
}

func TestResolveAssetHost(t *testing.T) {
	// An explicit --repo is the authentication authority, even when its host
	// differs from the asset URL host (the host-aware transport strips
	// credentials from any non-matching request host).
	resolved, err := resolveAssetHost("ghe.example.com/owner/repo", "https://github.com/owner/repo/assets/1")
	if err != nil {
		t.Fatalf("resolveAssetHost(explicit repo): %v", err)
	}
	if resolved.Host != "ghe.example.com" {
		t.Fatalf("explicit repo host = %q, want ghe.example.com", resolved.Host)
	}

	// An explicit but unparsable --repo is surfaced as an error rather than
	// being silently ignored.
	if _, err := resolveAssetHost("not a repo", "https://github.com/owner/repo/assets/1"); err == nil {
		t.Fatal("resolveAssetHost(invalid repo): expected error, got nil")
	} else if !strings.Contains(err.Error(), "not a repo") {
		t.Fatalf("resolveAssetHost(invalid repo): error = %v, want it to mention the repo", err)
	}

	// Without --repo, a recognized github.com asset URL resolves to github.com,
	// including via its user-attachment CDN hosts.
	for _, assetURL := range []string{
		"https://github.com/owner/repo/assets/1",
		"https://private-user-images.githubusercontent.com/1/2.png",
		"https://user-images.githubusercontent.com/1/2.png",
	} {
		resolved, err := resolveAssetHost("", assetURL)
		if err != nil {
			t.Fatalf("resolveAssetHost(%q): %v", assetURL, err)
		}
		if resolved.Host != "github.com" {
			t.Fatalf("resolveAssetHost(%q) host = %q, want github.com", assetURL, resolved.Host)
		}
	}

	// A non-HTTPS asset URL is rejected to avoid cleartext token transmission.
	if _, err := resolveAssetHost("", "http://github.com/owner/repo/assets/1"); err == nil {
		t.Fatal("resolveAssetHost(http): expected error, got nil")
	}

	// A malformed asset URL is rejected outright.
	if _, err := resolveAssetHost("", "::not a url"); err == nil {
		t.Fatal("resolveAssetHost(invalid url): expected error, got nil")
	}
}
