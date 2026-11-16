//go:build !dev

package main

import (
	"testing"

	"motty/internal/buildinfo"
)

// A plain go run is a release build even when it comes from a development tree.
func TestPlainBuildKeepsReleaseIdentity(t *testing.T) {
	if buildinfo.ProductName != "Motty" || mainWindowTitle() != "Motty" {
		t.Fatalf("ProductName = %q, window title = %q", buildinfo.ProductName, mainWindowTitle())
	}
}
