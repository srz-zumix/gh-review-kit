package attestation

import (
	"strings"
	"testing"
)

func TestResolveAssetHost(t *testing.T) {
	// An explicit --repo takes precedence over the asset URL host.
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
}
