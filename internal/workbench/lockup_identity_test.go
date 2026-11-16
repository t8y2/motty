package workbench

import "testing"

// The product's own logo and wordmark are the same picture in every build: a
// development build says which copy it is on the Dock icon (buildinfo.DockBadge),
// not by renaming the product inside its own interface.
func TestSidebarHeaderKeepsTheProductWordmarkInEveryBuild(t *testing.T) {
	state := newTestState(t, testOptions(t))
	_, texts := paintedTexts(state)
	if contains(texts, "Motty Dev") {
		t.Fatalf("the sidebar header renames the product: %q", texts)
	}
}
