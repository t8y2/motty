//go:build dev

package main

import (
	"testing"

	"motty/internal/buildinfo"
)

func TestDevBuildUsesDevelopmentIdentity(t *testing.T) {
	if buildinfo.ProductName != "Motty Dev" || mainWindowTitle() != "Motty Dev" {
		t.Fatalf("ProductName = %q, window title = %q", buildinfo.ProductName, mainWindowTitle())
	}
}
