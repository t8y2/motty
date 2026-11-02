package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

// searchResultPaths lists the results as folder/name.
func searchResultPaths(search *sftpSearch) []string {
	paths := make([]string, 0, len(search.results))
	for _, result := range search.results {
		item := search.items[result.item]
		paths = append(paths, filepath.ToSlash(filepath.Join(item.dir, item.name)))
	}
	return paths
}

func waitSearchDone(t *testing.T, state *State, session *Session, local bool) *sftpSearch {
	t.Helper()
	waitSFTP(t, state, session, func(view *sftpView) bool {
		folder := &view.sftpFolder
		if local {
			folder = &view.lfolder
		}
		return folder.search != nil && !folder.search.running
	})
	if local {
		return session.files.lfolder.search
	}
	return session.files.sftpFolder.search
}

// deepTree gives the test server a folder a few levels down, a hidden one and a
// link back up.
func deepTree(remote *workbenchSFTPRemote) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	remote.entries["/home/test/docs"] = append(remote.entries["/home/test/docs"],
		workbenchFileInfo{name: "api", mode: os.ModeDir | 0o755},
		workbenchFileInfo{name: "back", mode: os.ModeSymlink | 0o777},
	)
	remote.entries["/home/test/docs/api"] = []os.FileInfo{workbenchFileInfo{name: "Guide-v2.md", mode: 0o644}}
	remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{name: ".cache", mode: os.ModeDir | 0o755})
	remote.entries["/home/test/.cache"] = []os.FileInfo{workbenchFileInfo{name: "guide.tmp", mode: 0o644}}
}

func TestSFTPSearchFindsNamesInSubfoldersOverAChannelOfItsOwn(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	fake.mu.Lock()
	opens := fake.sftpOpens
	fake.mu.Unlock()
	clickSFTPRow(t, state, view, "notes.txt")

	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	search := waitSearchDone(t, state, session, false)

	// The server shows hidden files by default, so its hidden folders are read.
	// Nearer folders come first, and folders of one level in name order.
	want := []string{".cache/guide.tmp", "docs/guide.md", "docs/api/Guide-v2.md"}
	if got := searchResultPaths(search); !slices.Equal(got, want) {
		t.Fatalf("results = %q, want %q", got, want)
	}
	fake.mu.Lock()
	opened := fake.sftpOpens - opens
	fake.mu.Unlock()
	remote.mu.Lock()
	closed := remote.closed
	remote.mu.Unlock()
	if opened != 1 || !closed {
		t.Fatalf("the search opened %d channels of its own (closed %v), want one, closed when done", opened, closed)
	}
	if len(view.allRows) != 2 || selectedSFTPCount(view) != 0 {
		t.Fatalf("the folder's listing changed or kept a selection: %v, selected %d", rowNames(view.allRows), selectedSFTPCount(view))
	}
	if want := fmt.Sprintf(state.messages.Tools.FilesSearchDoneFmt, 3, len(search.items)); view.status != want {
		t.Fatalf("status = %q, want %q", view.status, want)
	}

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	toggle, ok := hitByID(&canvas, contextID(hitSFTPSearchToggle))
	if !ok || toggle.Label != state.messages.Tools.FilesSearchSubfolders {
		t.Fatalf("toggle hit = %+v, present %v", toggle, ok)
	}
	row, ok := hitByID(&canvas, contextID(hitSFTPSearchList.At(2)))
	if !ok || row.Label != "Guide-v2.md" || row.Hint != "docs/api/Guide-v2.md" {
		t.Fatalf("result hit = %+v, present %v", row, ok)
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPEntryList.At(0))); ok {
		t.Fatal("the folder's rows are painted under the results")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPDownload)); ok {
		t.Fatal("the selection bar is shown with the results")
	}

	replaceSFTPFilter(t, state, session, false, "V2")
	if got := searchResultPaths(search); !slices.Equal(got, []string{"docs/api/Guide-v2.md"}) || view.sftpFolder.search != search {
		t.Fatalf("a new query read the tree again or matched %q", got)
	}
}

func TestSFTPSearchLeadsToAResultInItsFolder(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "v2")
	waitSearchDone(t, state, session, false)

	result := state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2, Target: contextID(hitSFTPSearchList.At(0))})
	if !result.Handled {
		t.Fatal("the double click on a result was not handled")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs/api" })
	if view.sftpFolder.filterEditor.String() != "" || view.sftpFolder.search != nil {
		t.Fatalf("the search stayed: query %q", view.sftpFolder.filterEditor.String())
	}
	if view.selected < 0 || view.rows[view.selected].entry.Name != "Guide-v2.md" || !view.rows[view.selected].selected {
		t.Fatalf("the result was not selected in its folder: rows %v, selected %d", rowNames(view.rows), view.selected)
	}
}

func TestSFTPSearchOnThisComputerLeavesHiddenFoldersAloneUntilShown(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	for _, name := range []string{"photos/2024/beach.jpg", ".secrets/beach.key"} {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	markLocalHidden(t, filepath.Join(dir, ".secrets"))
	view := openLocalPane(t, state, session, dir)
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, true, "BEACH")
	search := waitSearchDone(t, state, session, true)
	if got := searchResultPaths(search); !slices.Equal(got, []string{"photos/2024/beach.jpg"}) {
		t.Fatalf("results with hidden files hidden = %q", got)
	}

	state.Click(hitSFTPHiddenToggle)
	search = waitSearchDone(t, state, session, true)
	if got := searchResultPaths(search); !slices.Equal(got, []string{".secrets/beach.key", "photos/2024/beach.jpg"}) {
		t.Fatalf("results with hidden files shown = %q", got)
	}

	state.Click(hitSFTPSearchToggle)
	if view.lfolder.search != nil || view.lfolder.searching() || len(view.lfolder.rows) != 0 {
		t.Fatalf("turning the switch off left the search or the query matched rows: %v", rowNames(view.lfolder.rows))
	}

	state.Click(hitSFTPSearchToggle)
	waitSearchDone(t, state, session, true)
	state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2, Target: contextID(hitSFTPSearchList.At(1))})
	folder := &view.lfolder
	if want := filepath.Join(dir, "photos", "2024"); folder.path != want || folder.selected < 0 || folder.rows[folder.selected].entry.Name != "beach.jpg" {
		t.Fatalf("the local result led to %q with rows %v, selected %d", folder.path, rowNames(folder.rows), folder.selected)
	}
}

func TestSFTPSearchStopsWithItsQueryAndItsPanel(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.readDelay = 20 * time.Millisecond
	remote.mu.Unlock()
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	if view.sftpFolder.search == nil {
		t.Fatal("no search started")
	}
	replaceSFTPFilter(t, state, session, false, "")
	if view.sftpFolder.search != nil || !strings.Contains(view.status, "2") {
		t.Fatalf("clearing the query left the search; status %q", view.status)
	}
	replaceSFTPFilter(t, state, session, false, "guide")
	search := view.sftpFolder.search
	state.closeSFTP(session)
	if search == nil || view.sftpFolder.search != nil {
		t.Fatal("closing the panel left the search running")
	}
}

func TestSFTPSearchShowsAtMostItsLimitButCountsEveryMatch(t *testing.T) {
	search := &sftpSearch{selected: -1}
	search.match("log")
	found := make([]sftpclient.Found, maxSearchResults+10)
	for index := range found {
		found[index] = sftpclient.Found{Dir: "var", Entry: sftpclient.Entry{Name: fmt.Sprintf("app-%d.log", index)}}
	}
	search.add(found)
	if len(search.results) != maxSearchResults || search.matched != maxSearchResults+10 {
		t.Fatalf("results %d, matched %d", len(search.results), search.matched)
	}
}
