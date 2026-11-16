//go:build darwin

package workbench

import (
	"runtime"
	"testing"
)

// An Intel build on Apple silicon runs translated, and an update for it must be
// the native package: asking for the architecture in the process would keep
// every later version translated too.
func TestAnUpdateReplacesTheTranslatedBuildWithTheNativeOne(t *testing.T) {
	original := runningTranslated
	defer func() { runningTranslated = original }()

	runningTranslated = func() bool { return true }
	if got := updateArch(); got != "arm64" {
		t.Fatalf("a translated build asks for the %q package, want arm64", got)
	}
	runningTranslated = func() bool { return false }
	if got := updateArch(); got != runtime.GOARCH {
		t.Fatalf("a native build asks for the %q package, want %q", got, runtime.GOARCH)
	}
}
