package workbench

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

// searchResultPaths lists the results as folder/name.
func searchResultPaths(search *sftpSearch) []string {
	paths := make([]string, 0, len(search.rows))
	for _, row := range search.rows {
		paths = append(paths, path.Join(row.where, row.entry.Name))
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

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	toggle, ok := hitByID(&canvas, contextID(hitSFTPSearchToggle))
	if !ok || toggle.Label != state.messages.Tools.FilesSearchSubfolders {
		t.Fatalf("toggle hit = %+v, present %v", toggle, ok)
	}
	// The results are the folder's list: its rows, holding the matches.
	row, ok := hitByID(&canvas, contextID(hitSFTPEntryList.At(2)))
	if !ok || row.Label != "Guide-v2.md" || row.Hint != "docs/api" {
		t.Fatalf("result hit = %+v, present %v", row, ok)
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPEntryList.At(3))); ok {
		t.Fatal("the list has a row beyond the three results")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPDownload)); ok {
		t.Fatal("the selection bar is shown with the results")
	}

	// A press on a result selects it, and only it: nothing of the folder is
	// selected for the buttons and keys that act on a selection.
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, Target: contextID(hitSFTPEntryList.At(1))})
	if search.selected != 1 || !search.rows[1].selected || selectedSFTPCount(view) != 0 {
		t.Fatalf("selected result %d, folder selection %d", search.selected, selectedSFTPCount(view))
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

	// A server that is busy answers neither a double click nor Enter, as it
	// does not for the folder's rows.
	view.busy = true
	target := contextID(hitSFTPEntryList.At(0))
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: target})
	state.Key(gfx.KeyEvent{Target: target, Key: gfx.KeyEnter})
	if view.path != "/home/test" || view.sftpFolder.search == nil || view.sftpFolder.filterEditor.String() != "v2" {
		t.Fatalf("a busy server was navigated: path %q, query %q", view.path, view.sftpFolder.filterEditor.String())
	}
	view.busy = false

	state.Key(gfx.KeyEvent{Target: target, Key: gfx.KeyEnter})
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs/api" })
	if view.sftpFolder.filterEditor.String() != "" || view.sftpFolder.search != nil {
		t.Fatalf("the search stayed: query %q", view.sftpFolder.filterEditor.String())
	}
	if view.selected < 0 || view.rows[view.selected].entry.Name != "Guide-v2.md" || !view.rows[view.selected].selected {
		t.Fatalf("the result was not selected in its folder: rows %v, selected %d", rowNames(view.rows), view.selected)
	}
}

func TestSFTPSearchLeadsToAFolderResultIntoThatFolder(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "api")
	waitSearchDone(t, state, session, false)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: contextID(hitSFTPEntryList.At(0))})
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs/api" })
	if view.selected >= 0 && view.rows[view.selected].selected {
		t.Fatalf("a folder result selected %q in the folder it opened", view.rows[view.selected].entry.Name)
	}
}

func TestSFTPSearchRevealBelongsToItsNavigation(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "v2")
	waitSearchDone(t, state, session, false)

	// The folder the result is in cannot be read, so the navigation fails; the
	// wish to select the file must not wait for some later visit.
	remote.mu.Lock()
	remote.unreadable = map[string]bool{"/home/test/docs/api": true}
	remote.mu.Unlock()
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: contextID(hitSFTPEntryList.At(0))})
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
	if view.path != "/home/test" {
		t.Fatalf("the failed navigation left %q", view.path)
	}
	remote.mu.Lock()
	remote.unreadable = nil
	remote.mu.Unlock()
	state.navigateSFTPDirectory(view, "/home/test/docs/api")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs/api" })
	for _, row := range view.rows {
		if row.selected {
			t.Fatalf("%q was selected by a reveal that had failed", row.entry.Name)
		}
	}
}

func TestSFTPSearchRevealIsClaimedByTheNavigationStartedForIt(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.sftpFolder.reveal = sftpReveal{dir: "/home/test/docs", name: "guide.md"}
	nav := state.beginSFTPNavigation(view, false, "/home/test/docs", sftpNavigationManual, 0)
	if nav.reveal.name != "guide.md" || view.sftpFolder.reveal.name != "" {
		t.Fatalf("the navigation did not take the reveal: %+v, folder keeps %+v", nav.reveal, view.sftpFolder.reveal)
	}
	// Any other navigation, such as the folder being read again, has none to
	// spend, so one that arrives first cannot use it up.
	if other := state.beginSFTPNavigation(view, false, "/home/test", sftpNavigationPassive, 0); other.reveal.name != "" {
		t.Fatalf("a later navigation was given %+v", other.reveal)
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
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: contextID(hitSFTPEntryList.At(1))})
	folder := &view.lfolder
	if want := filepath.Join(dir, "photos", "2024"); folder.path != want || folder.selected < 0 || folder.rows[folder.selected].entry.Name != "beach.jpg" {
		t.Fatalf("the local result led to %q with rows %v, selected %d", folder.path, rowNames(folder.rows), folder.selected)
	}
}

func TestSFTPSearchReadsTheTreeOnceWhateverTheQueryBecomes(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	search := waitSearchDone(t, state, session, false)
	fake.mu.Lock()
	opens := fake.sftpOpens
	fake.mu.Unlock()
	reads := remote.readCount("/home/test/docs/api")

	replaceSFTPFilter(t, state, session, false, "")
	if view.sftpFolder.search != search || view.sftpFolder.searching() || len(view.rows) != 2 {
		t.Fatalf("clearing the query dropped what was read, or still shows results: rows %v", rowNames(view.rows))
	}
	if want := fmt.Sprintf(state.messages.SFTPItemsFmt, 2); view.status != want {
		t.Fatalf("status with no query = %q, want %q", view.status, want)
	}
	replaceSFTPFilter(t, state, session, false, "v2")
	if got := searchResultPaths(search); !slices.Equal(got, []string{"docs/api/Guide-v2.md"}) || view.sftpFolder.search != search {
		t.Fatalf("typing again matched %q", got)
	}
	fake.mu.Lock()
	reopened := fake.sftpOpens - opens
	fake.mu.Unlock()
	if reopened != 0 || remote.readCount("/home/test/docs/api") != reads {
		t.Fatalf("typing again read the tree again: %d channels, %d reads", reopened, remote.readCount("/home/test/docs/api")-reads)
	}

	// Going somewhere else, or turning the switch off, is what drops it.
	state.Click(hitSFTPSearchToggle)
	if view.sftpFolder.search != nil {
		t.Fatal("turning the switch off kept the reading")
	}
	state.Click(hitSFTPSearchToggle)
	if view.sftpFolder.search == nil || view.sftpFolder.search == search {
		t.Fatal("turning the switch on again did not read the tree afresh")
	}
	other := view.sftpFolder.search
	state.navigateSFTPDirectory(view, "/home/test/docs")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if view.sftpFolder.search == other {
		t.Fatal("a search of another folder was kept")
	}
	search = view.sftpFolder.search
	state.closeSFTP(session)
	if search == nil || view.sftpFolder.search != nil {
		t.Fatal("closing the panel left the search running")
	}
}

func TestSFTPSearchShowsWhatAFolderGaveWhileTheNextIsStillBeingRead(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	release := make(chan struct{})
	remote.mu.Lock()
	remote.onRead = func(dir string) {
		if dir == "/home/test/docs/api" {
			<-release // a big folder on a slow link
		}
	}
	remote.mu.Unlock()
	defer func() {
		defer func() { _ = recover() }() // already released
		close(release)
	}()
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	waitSFTP(t, state, session, func(view *sftpView) bool {
		search := view.sftpFolder.search
		return search != nil && len(search.rows) == 2
	})
	search := view.sftpFolder.search
	if !search.running {
		t.Fatal("the search finished while a folder was still being read")
	}
	want := fmt.Sprintf(state.messages.Tools.FilesSearchScanningFmt, 2, len(search.items))
	if view.status != want {
		t.Fatalf("status while reading = %q, want %q", view.status, want)
	}
	close(release)
	waitSearchDone(t, state, session, false)
}

func TestSFTPSearchStatusSurvivesTheFolderBeingListedAgain(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	remote.mu.Lock()
	remote.unreadable = map[string]bool{"/home/test/docs/api": true}
	remote.mu.Unlock()
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	search := waitSearchDone(t, state, session, false)
	status := view.status
	if search.summary.Failed != 1 || !strings.Contains(status, fmt.Sprintf(state.messages.Tools.FilesSearchFailedFmt, 1)) {
		t.Fatalf("unreadable folders are not in the status %q", status)
	}

	// The folder on show is read again, as it is when it changes on the server.
	state.updateSFTPRows(view, []sftpclient.Entry{{Name: "notes.txt", Mode: 0o644}, {Name: "docs", Mode: os.ModeDir | 0o755}, {Name: "new.txt", Mode: 0o644}})
	if view.status != status || view.sftpFolder.search != search {
		t.Fatalf("listing again changed the status to %q or dropped the search", view.status)
	}
	state.showSFTPListing(view, view.path, []sftpclient.Entry{{Name: "notes.txt", Mode: 0o644}})
	if view.status != status {
		t.Fatalf("showing a listing changed the status to %q, want %q", view.status, status)
	}
}

func TestSFTPSearchSaysWhyWhenTheFolderCannotBeRead(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.unreadable = map[string]bool{"/home/test": true}
	remote.mu.Unlock()
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	waitSearchDone(t, state, session, false)
	if want := fmt.Sprintf(state.messages.Tools.FilesSearchUnavailableFmt, os.ErrPermission); view.status != want {
		t.Fatalf("status = %q, want %q", view.status, want)
	}
}

func TestSFTPSearchShowsAtMostItsLimitButCountsEveryMatch(t *testing.T) {
	search := &sftpSearch{selected: -1}
	search.match("log")
	items := make([]sftpSearchItem, maxSearchResults+10)
	for index := range items {
		name := fmt.Sprintf("app-%d.log", index)
		items[index] = sftpSearchItem{dir: "var", name: name, folded: name}
	}
	search.add(items)
	if len(search.rows) != maxSearchResults || search.matched != maxSearchResults+10 {
		t.Fatalf("results %d, matched %d", len(search.rows), search.matched)
	}
}

func TestElideSFTPFolderKeepsTheFirstAndTheLastFolders(t *testing.T) {
	fits := func(limit int) func(string) bool { return func(text string) bool { return len([]rune(text)) <= limit } }
	for _, test := range []struct {
		dir   string
		limit int
		want  string
	}{
		{"docs", 4, "docs"},
		{"projects/server/internal/handlers", 100, "projects/server/internal/handlers"},
		{"projects/server/internal/handlers", 28, "projects/…/internal/handlers"},
		{"projects/server/internal/handlers", 20, "projects/…/handlers"},
		{"projects/server/internal/handlers", 3, "projects/…/handlers"}, // the room's end does the rest
		{"a/b", 1, "a/b"},
	} {
		if got := elideSFTPFolder(test.dir, fits(test.limit)); got != test.want {
			t.Errorf("elideSFTPFolder(%q, %d) = %q, want %q", test.dir, test.limit, got, test.want)
		}
	}
}
