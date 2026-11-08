package insights

import "testing"

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
