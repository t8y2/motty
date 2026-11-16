//go:build !windows

package workbench

import "testing"

func markLocalHidden(_ *testing.T, _ string) {}

func TestSFTPHiddenLocalPredicateUsesLeadingDots(t *testing.T) {
	if !localFileHidden(".secret", workbenchFileInfo{name: ".secret"}) || localFileHidden("visible.txt", workbenchFileInfo{name: "visible.txt"}) {
		t.Fatal("the local platform hidden predicate does not follow leading dots")
	}
}
