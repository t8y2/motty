package workbench

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

// openLargeSFTP opens the file panel on a directory of count entries.
func openLargeSFTP(t testing.TB, count int) (*State, *Session) {
	t.Helper()
	state, _, fake, _ := liveSession(t)
	list := make([]os.FileInfo, 0, count)
	for index := range count {
		list = append(list, workbenchFileInfo{name: fmt.Sprintf("entry-%05d.txt", index), size: int64(index), mode: 0o644, when: time.Unix(1_700_000_000+int64(index)*60, 0)})
	}
	fake.mu.Lock()
	fake.sftp = &workbenchSFTPRemote{cwd: "/big", entries: map[string][]os.FileInfo{"/big": list}, files: map[string][]byte{}}
	fake.mu.Unlock()
	state.Click(hitSFTPOpen)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == count })
	t.Cleanup(func() { state.closeSFTP(session) })
	return state, session
}

func TestSFTPListOfThousandsPaintsOnlyWhatIsVisible(t *testing.T) {
	state, session := openLargeSFTP(t, 5000)
	// The List is declared inside the panel's region of the workbench page. Viva
	// reads a nested area's viewport after the paint that lays it out, so the
	// first paint estimates it from the page; measure the steady state, which is
	// what every later frame paints.
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	view := session.files
	if view.visibleRows <= 0 || view.visibleRows >= 100 {
		t.Fatalf("visible rows = %d of %d", view.visibleRows, len(view.rows))
	}
	names := 0
	for _, text := range canvas.Texts() {
		if strings.HasPrefix(text.Value, "entry-") {
			names++
		}
	}
	// The renderer keeps one partially visible row and a small scroll-ahead
	// buffer, but must never build the whole directory. A List declares two
	// rows beyond each edge of the viewport.
	if names < view.visibleRows || names > view.visibleRows+6 {
		t.Fatalf("%d names painted for %d visible rows", names, view.visibleRows)
	}
}

func TestSFTPEditorShowsWhichFileIsOpen(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	openSFTPTextEditor(t, state, session, "notes.txt")
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	found := false
	for _, text := range canvas.Texts() {
		found = found || text.Value == "notes.txt" && text.Bounds.Width > 0
	}
	if !found {
		t.Fatal("the editor does not show the name of the file")
	}
}

func TestRemoteToolsTabsAndSortKeepApartInANarrowWindow(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 520, Height: 380})
	tabs, ok := settingsChoiceIn(&canvas, state.messages.Tools.RemoteTabContainers)
	sort, sortOK := hitByID(&canvas, hitRemoteSortMemory)
	if !ok || !sortOK {
		t.Fatal("the tabs or the sort order are not painted")
	}
	if tabs.Bounds.X+tabs.Bounds.Width > sort.Bounds.X+0.5 && sort.Bounds.X+sort.Bounds.Width > tabs.Bounds.X {
		t.Fatalf("tabs %v and sort order %v overlap", tabs.Bounds, sort.Bounds)
	}
}

func benchmarkPaintLargeSFTP(b *testing.B, scrolled bool) {
	state, session := openLargeSFTP(b, 5000)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if scrolled {
		session.files.first = 2500
	}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func BenchmarkPaintSFTPPage5000(b *testing.B)         { benchmarkPaintLargeSFTP(b, false) }
func BenchmarkPaintSFTPPage5000Scrolled(b *testing.B) { benchmarkPaintLargeSFTP(b, true) }
