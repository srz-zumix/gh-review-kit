package insights

import (
	"strings"
	"testing"
)

func TestBundleGroupNameDistinguishesCollidingKeys(t *testing.T) {
	a := bundleGroupName(".github/instructions/foo.bar.instructions.md")
	b := bundleGroupName(".github/instructions/foo_bar.instructions.md")
	if a == b {
		t.Fatalf("bundleGroupName collided: %q", a)
	}
	if got := bundleGroupName("foo_bar"); got != "foo_bar" {
		t.Errorf("bundleGroupName(%q) = %q, want unchanged", "foo_bar", got)
	}
	if bundleGroupName("foo.bar") == bundleGroupName("foo_bar") {
		t.Errorf("bundleGroupName(foo.bar) collided with foo_bar")
	}
}

func TestBundleGroupNameUniqueAndBounded(t *testing.T) {
	a := bundleGroupName("foo.bar")
	b := bundleGroupName(a)
	if a == b {
		t.Fatalf("collision between %q and %q: %q", "foo.bar", a, a)
	}
	if got := bundleGroupName("foo_bar"); got != "foo_bar" {
		t.Fatalf("plain key changed: %q", got)
	}
	long := strings.Repeat("a", 236) + "."
	name := bundleGroupName(long)
	if n := len(name + "-0001.jsonl"); n > 255 {
		t.Fatalf("filename too long: %d", n)
	}
	if bundleGroupName(strings.Repeat("a", 300)) == bundleGroupName(strings.Repeat("a", 301)) {
		t.Fatalf("truncated long keys collided")
	}
}
