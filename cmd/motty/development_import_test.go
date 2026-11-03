//go:build darwin || windows || linux

package main

import (
	"path/filepath"
	"testing"

	"motty/internal/buildinfo"
)

func TestDevelopmentImportDiscoveryIsLimitedToDefaultDevelopmentProfile(t *testing.T) {
	explicit, err := developmentImportSource(t.TempDir())
	if err != nil || explicit != nil {
		t.Fatalf("explicit profile must not discover real data: %+v %v", explicit, err)
	}
	source, err := developmentImportSource("")
	if err != nil {
		t.Fatal(err)
	}
	if buildinfo.Development {
		if source == nil || filepath.Base(source.Directory) != "motty" {
			t.Fatalf("missing release source: %+v", source)
		}
	} else if source != nil {
		t.Fatal("release must not offer development migration")
	}
}
