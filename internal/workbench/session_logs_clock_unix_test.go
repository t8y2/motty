//go:build !windows

package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionLogsLocationReloadsChangedZoneFile(t *testing.T) {
	utc, err := os.ReadFile("/usr/share/zoneinfo/UTC")
	if err != nil {
		t.Fatal(err)
	}
	ny, err := os.ReadFile("/usr/share/zoneinfo/America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "localtime")
	if err := os.WriteFile(path, utc, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TZ", ":"+path)
	source, location := loadSessionLogLocation("", nil)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if _, offset := at.In(location).Zone(); offset != 0 {
		t.Fatal("initial zone file was not loaded")
	}
	if err := os.WriteFile(path, ny, 0600); err != nil {
		t.Fatal(err)
	}
	next, changed := loadSessionLogLocation(source, location)
	if _, offset := at.In(changed).Zone(); next == source || changed == location || offset != -5*3600 {
		t.Fatal("replacement of a zone file was not observed")
	}
	if _, cached := loadSessionLogLocation(next, changed); cached != changed {
		t.Fatal("unchanged zone file caused repeated view invalidations")
	}
	data, err := readSessionLogSystemZone()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.LoadLocationFromTZData("Local", data); err != nil {
		t.Fatalf("system localtime is not a usable location: %v", err)
	}
}
