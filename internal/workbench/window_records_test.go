package workbench

import (
	"path/filepath"
	"testing"

	"motty/internal/buildinfo"
)

// windowsOnOneStorePaths is the store two windows of one process share, with a
// host saved so each window has something to open.
func windowsOnOneStorePaths(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.db")
	if err := saveState(path, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	return path
}

// A window the user closed leaves a gap in the numbering. The next launch closes
// it, so a window's identity stays its ordinal and no window opens where none
// was.
func TestRestoreWindowCountClosesTheGapAClosedWindowLeft(t *testing.T) {
	path := windowsOnOneStorePaths(t)
	dir := filepath.Dir(path)
	options := func(id int) Options {
		return Options{
			KnownHostsPath: filepath.Join(dir, "known_hosts"),
			StatePath:      path,
			WindowID:       id,
			Blank:          true,
		}
	}
	first := newTestState(t, options(1))
	second := newTestState(t, options(2))
	third := newTestState(t, options(3))
	for _, state := range []*State{first, second, third} {
		state.save()
		state.Close()
	}
	// The user closes the second window, so its record goes by itself.
	second.DropWindowRecord()

	count, err := RestoreWindowCount(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("restore opened %d windows, want 2", count)
	}
	// The third window is now number 2, and its own tabs came with it.
	reopened := newTestState(t, options(2))
	if reopened.configBase.windowName != windowKey(2, buildinfo.Development) {
		t.Fatalf("window 2 reads record %q", reopened.configBase.windowName)
	}
}

// A record past the number of windows belongs to no window of the run, so the
// launch drops it instead of leaving it to grow the store.
func TestRestoreWindowCountDropsARecordNoWindowOpens(t *testing.T) {
	path := windowsOnOneStorePaths(t)
	dir := filepath.Dir(path)
	for id := 1; id <= 3; id++ {
		state := newTestState(t, Options{
			KnownHostsPath: filepath.Join(dir, "known_hosts"),
			StatePath:      path,
			WindowID:       id,
			Blank:          true,
		})
		state.save()
		state.Close()
	}
	// A store whose last window closed without dropping its record, which is
	// what a quit after a close leaves behind.
	if _, err := RestoreWindowCount(path); err != nil {
		t.Fatal(err)
	}
	count, err := RestoreWindowCount(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("restore opened %d windows, want the 3 records it found", count)
	}
}

// Every launch opens a window even though the store holds none: a first run, and
// one where the last window closed normally, must still show something.
func TestRestoreWindowCountOpensOneWindowWithoutRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	if err := saveState(path, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	count, err := RestoreWindowCount(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("restore opened %d windows without a record, want 1", count)
	}
}

// openWindowIn opens window id of one process over the shared store, with a
// dialer whose connections the caller can drive.
func openWindowIn(t *testing.T, path string, id int) (*State, *world) {
	t.Helper()
	dialer, options := newWorld(t)
	options.StatePath = path
	options.KnownHostsPath = filepath.Join(filepath.Dir(path), "known_hosts")
	options.WindowID = id
	return newTestState(t, options), dialer
}

// A launch brings each window back with the tabs that window had, not the last
// one that saved. This is the defect the whole change is for: before it, every
// window read the same document and overwrote the others.
func TestEachWindowRestoresItsOwnTabs(t *testing.T) {
	path := windowsOnOneStorePaths(t)

	first, firstDialer := openWindowIn(t, path, 1)
	liveTab(t, first, firstDialer, 0)
	if !first.saveResult() {
		t.Fatal("the first window did not save")
	}
	firstAddress := first.sessions[0].Address
	first.Close()

	second, secondDialer := openWindowIn(t, path, 2)
	// A window with no record of its own starts empty rather than copying the
	// first window's tab.
	if len(second.sessions) != 0 {
		t.Fatalf("the second window opened with %d tabs, want none", len(second.sessions))
	}
	liveTab(t, second, secondDialer, 2)
	if !second.saveResult() {
		t.Fatal("the second window did not save")
	}
	secondAddress := second.sessions[0].Address
	second.Close()

	restored, _ := openWindowIn(t, path, 1)
	if len(restored.sessions) != 1 || restored.sessions[0].Address != firstAddress {
		t.Fatalf("window 1 restored %+v, want the %s tab alone", restored.sessions, firstAddress)
	}

	other, _ := openWindowIn(t, path, 2)
	if len(other.sessions) != 1 || other.sessions[0].Address != secondAddress {
		t.Fatalf("window 2 restored %+v, want the %s tab alone", other.sessions, secondAddress)
	}
	if firstAddress == secondAddress {
		t.Fatal("both windows opened the same host, so the tabs cannot be told apart")
	}
}
