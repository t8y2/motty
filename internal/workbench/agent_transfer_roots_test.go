package workbench

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

// A saved folder that has since been deleted must not stop the service, and the
// folders that remain usable must still be offered, with the command-line roots
// kept and not duplicated.
func TestSavedTransferRootsSkipUnusableFolders(t *testing.T) {
	good, gone := t.TempDir(), filepath.Join(t.TempDir(), "missing")
	value, err := json.Marshal([]string{gone, good, "relative"})
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	saved := savedTransferRoots(string(value), &log)
	if len(saved) != 1 || saved[0] != good {
		t.Fatalf("saved roots = %v, want only %s", saved, good)
	}
	if log.Len() == 0 {
		t.Fatal("skipped folders were not reported")
	}
	merged := mergeTransferRoots([]string{good}, saved)
	if len(merged) != 1 {
		t.Fatalf("merged roots = %v, want no duplicate", merged)
	}
	if got := parseTransferRoots("not json"); got != nil {
		t.Fatalf("unreadable setting gave %v", got)
	}
}
