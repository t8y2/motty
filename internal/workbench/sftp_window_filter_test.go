//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/keymap"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func dualClickID(t *testing.T, view *headless.Session, id gfx.ID) {
	t.Helper()
	hit := dualHit(t, view, id)
	if err := view.Click(hit.X+hit.Width/2, hit.Y+hit.Height/2); err != nil {
		t.Fatal(err)
	}
}
func dualFind(t *testing.T, view *headless.Session, w *sftpWindow, local bool) gfx.ID {
	t.Helper()
	folder := w.folder(local)
	if len(folder.rows) > 0 {
		dualClickID(t, view, sftpWindowRows[sideIndex(local)].At(0))
	}
	if _, err := view.Driver().ShortcutChord(keymap.Primary, 'f'); err != nil {
		t.Fatal(err)
	}
	focused := view.Snapshot().Focused
	input := w.textInput(app.TextInputContext{ID: focused})
	if !folder.filterOpen || input == nil || input.OnEvent == nil {
		t.Fatalf("primary+F did not open native filter local=%t: open=%t focus=%v input=%+v", local, folder.filterOpen, focused, input)
	}
	return focused
}
func dualReplaceFilter(t *testing.T, view *headless.Session, value string) {
	t.Helper()
	if _, err := view.Driver().ShortcutChord(keymap.Primary, 'a'); err != nil {
		t.Fatal(err)
	}
	if err := view.Type(value); err != nil {
		t.Fatal(err)
	}
}
func dualText(t *testing.T, view *headless.Session, want string) {
	t.Helper()
	for _, label := range view.Snapshot().Texts {
		if label.Value == want {
			return
		}
	}
	t.Fatalf("missing visible text %q", want)
}

// Catches active-session coupling, text shortcuts selecting files, invisible
// transfer targets, and per-keystroke remote listings.
func TestSFTPWindowNativeFiltersIndependentAndVisibilitySafe(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	state.activeSessionID = 0
	before := remote.readCount(w.view.path)
	localSeq := w.view.localSeq
	localInput := dualFind(t, view, w, true)
	dualReplaceFilter(t, view, "中文")
	if got := fmt.Sprint(rowNames(w.view.lfolder.rows)); got != "[中文上传.yml]" {
		t.Fatalf("local rows=%s", got)
	}
	dualText(t, view, fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, 1, 3))
	if len(w.view.rows) != 2 || w.view.filterEditor.String() != "" {
		t.Fatal("local query leaked into remote")
	}
	dualClickID(t, view, sftpWindowRows[0].At(0))
	if !w.canTransfer(true) {
		t.Fatal("visible local row is not transferable")
	}
	dualClickID(t, view, localInput)
	dualReplaceFilter(t, view, "missing")
	if w.canTransfer(true) || w.selection(true).count != 0 {
		t.Fatal("filtered-out local row stayed actionable")
	}
	w.queueSelection(true)
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("invisible selection queued transfer")
	}
	dualReplaceFilter(t, view, "中文")
	remoteInput := dualFind(t, view, w, false)
	dualReplaceFilter(t, view, "NOTES")
	if got := fmt.Sprint(rowNames(w.view.rows)); got != "[notes.txt]" {
		t.Fatalf("remote rows=%s", got)
	}
	if w.view.lfolder.filterEditor.String() != "中文" {
		t.Fatal("remote replaced local query")
	}
	dualClickID(t, view, sftpWindowRows[1].At(0))
	if !w.canTransfer(false) {
		t.Fatal("visible remote row is not transferable")
	}
	dualClickID(t, view, remoteInput)
	dualReplaceFilter(t, view, "notes")
	if !w.canTransfer(false) {
		t.Fatal("visible match lost selection on query change")
	}
	if remote.readCount(w.view.path) != before || w.view.localSeq != localSeq {
		t.Fatal("search triggered directory I/O")
	}
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if !w.view.filterOpen || w.view.filterEditor.String() != "" || len(w.view.rows) != 2 {
		t.Fatal("first Esc did not clear remote query")
	}
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if w.view.filterOpen || w.closed {
		t.Fatal("second Esc did not dismiss only filter")
	}
	dualClickID(t, view, localInput)
	if _, err := view.Driver().ShortcutChord(keymap.Primary, 'l'); err != nil {
		t.Fatal(err)
	}
	if view.Snapshot().Focused != sftpWindowPath[0] {
		t.Fatal("path shortcut lost active side")
	}
	if w.view.lfolder.path != dir {
		t.Fatal("filter changed directory")
	}
	dualFind(t, view, w, false)
	dualReplaceFilter(t, view, "notes")
	dualClickID(t, view, sftpWindowRows[1].At(0))
	w.queueSelection(false)
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 1 && !sftpTransfersActive(w.view.transfers) })
	data, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || string(data) != "hello world" {
		t.Fatalf("filtered transfer data=%q err=%v", data, err)
	}
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.pendingLocal == "" && !w.localRefresh })
	dualClickID(t, view, dualRow(t, w, true, "中文上传.yml"))
	w.queueSelection(true)
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 2 && !sftpTransfersActive(w.view.transfers) })
	if got := remoteText(remote, "/home/test/中文上传.yml"); got != "name: 中文\n" {
		t.Fatalf("filtered upload data=%q", got)
	}
}

func TestSFTPWindowMoreMenuHiddenAndClearArePerSide(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	file := filepath.Join(dir, ".secret")
	if err := os.WriteFile(file, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	markLocalHidden(t, file)
	remote.mu.Lock()
	remote.entries[w.view.path] = append(remote.entries[w.view.path], workbenchFileInfo{name: ".env", mode: 0600})
	remote.mu.Unlock()
	w.refresh(true)
	w.refresh(false)
	dualWait(t, state, w, view, func() bool { return !w.view.busy && len(w.view.allRows) == 3 && len(w.view.lfolder.allRows) == 4 })
	if w.view.lfolder.showHidden || !w.view.showHidden {
		t.Fatal("hidden defaults changed")
	}
	if err := view.Resize(640, 600); err != nil {
		t.Fatal(err)
	}
	clickSide := func(local bool, label string) {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.Label == label && !hit.Block && (hit.Bounds.X < w.ui.list[1].X) == local {
				dualClickID(t, view, hit.ID)
				return
			}
		}
		t.Fatalf("missing side %t action %q", local, label)
	}
	before := remote.readCount(w.view.path)
	clickSide(true, state.messages.Tools.FilesMore)
	clickSide(true, state.messages.Tools.FilesShowHidden)
	if !w.view.lfolder.showHidden || len(w.view.lfolder.rows) != 4 || !w.view.showHidden {
		t.Fatal("local hidden action changed wrong side")
	}
	dualClickID(t, view, dualRow(t, w, true, ".secret"))
	clickSide(true, state.messages.Tools.FilesHideHidden)
	if w.canTransfer(true) {
		t.Fatal("hidden local selection remained transferable")
	}
	if w.view.lfolder.showHidden || len(w.view.lfolder.rows) != 3 {
		t.Fatal("Eye did not hide local entries")
	}
	dualClickID(t, view, dualRow(t, w, false, ".env"))
	clickSide(false, state.messages.Tools.FilesHideHidden)
	if w.canTransfer(false) {
		t.Fatal("hidden remote selection remained transferable")
	}
	if w.view.showHidden || len(w.view.rows) != 2 || w.view.lfolder.showHidden {
		t.Fatal("remote hidden action leaked")
	}
	localInput := dualFind(t, view, w, true)
	dualReplaceFilter(t, view, "tree")
	clickSide(true, state.messages.Tools.FilesFilterClear)
	if w.view.lfolder.filterEditor.String() != "" || len(w.view.lfolder.rows) != 3 || !w.view.lfolder.filterOpen {
		t.Fatal("clear did not restore local rows")
	}
	dualClickID(t, view, localInput)
	dualReplaceFilter(t, view, strings.Repeat("中", 300))
	if w.view.lfolder.filterEditor.RuneCount() != sftpFilterMaxRunes {
		t.Fatal("native query limit lost")
	}
	if remote.readCount(w.view.path) != before {
		t.Fatal("hidden/filter actions performed remote I/O")
	}
}

func TestSFTPPanelMoreSortPreservesAnchorAndRefresh(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	entries := []sftpclient.Entry{{Name: "z.txt", Size: 10, ModTime: time.Unix(1, 0)}, {Name: "a.txt", Size: 30, ModTime: time.Unix(3, 0)}, {Name: "m.txt", Size: 20, ModTime: time.Unix(2, 0)}, {Name: "folder", Mode: os.ModeDir | 0755}}
	state.showSFTPListing(view, view.path, entries)
	pressSFTPRow(state, view, sftpRowIndex(t, view, "m.txt"), 0)
	// The menu's sort rows are identified by column, exactly as the panel paints
	// them: its labels also name the column headers, which are painted before it.
	sortBy := func(column int, label string) {
		t.Helper()
		state.Click(contextID(hitSFTPHeaderMore))
		state.Click(contextID(sftpHeaderSortHits.At(column)))
		if view.active().headerMenuOpen {
			t.Fatalf("panel More did not take sort %q", label)
		}
	}
	sortBy(int(sftpColumnSize), state.messages.SFTPWindow.Size)
	if got := fmt.Sprint(rowNames(view.rows)); got != "[folder z.txt m.txt a.txt]" {
		t.Fatalf("size order=%s", got)
	}
	if view.anchor < 0 || view.selected < 0 || view.rows[view.anchor].entry.Name != "m.txt" || view.rows[view.selected].entry.Name != "m.txt" || !view.rows[view.selected].selected {
		t.Fatal("sort lost selection anchor")
	}
	sortBy(int(sftpColumnModified), state.messages.SFTPWindow.Modified)
	state.showSFTPListing(view, view.path, entries)
	if got := fmt.Sprint(rowNames(view.rows)); got != "[folder z.txt m.txt a.txt]" {
		t.Fatalf("refresh lost modified order=%s", got)
	}
	sortBy(int(sftpColumnName), state.messages.SFTPWindow.Name)
	if got := fmt.Sprint(rowNames(view.rows)); got != "[folder a.txt m.txt z.txt]" {
		t.Fatalf("name order=%s", got)
	}
}

func TestSFTPWindowHeaderMenuHasUniqueHitsAndNarrowTransfers(t *testing.T) {
	_, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if err := view.Resize(640, 600); err != nil {
		t.Fatal(err)
	}
	dualClickID(t, view, sftpWindowMore[1])
	seen := make(map[gfx.ID]bool)
	for _, hit := range view.Snapshot().Hits {
		if hit.Block || hit.ID.IsZero() {
			continue
		}
		if seen[hit.ID] {
			t.Fatalf("header popup reuses visible hit ID %v", hit.ID)
		}
		seen[hit.ID] = true
	}
	for _, id := range []gfx.ID{sftpWindowFilterToggle[1], hitSFTPWindowUpload, hitSFTPWindowDownload, sftpWindowNewFolder[1], hitSFTPNewLink} {
		dualHit(t, view, id)
	}
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if w.headerMenuOpen() {
		t.Fatal("Escape left header menu open")
	}
}

func TestSFTPWindowFilterCaretKeepsPreciseScrollAndIME(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	entries := make([]sftpclient.Entry, 200)
	for i := range entries {
		entries[i].Name = fmt.Sprintf("配置-%03d.txt", i)
	}
	state.showSFTPListing(w.view, w.view.path, entries)
	w.window.Invalidate()
	view.Driver().Pump()
	id := dualFind(t, view, w, false)
	dualReplaceFilter(t, view, "配置")
	w.ui.offset[1] = 60.5
	w.view.first = 2
	input := w.textInput(app.TextInputContext{ID: id})
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextSetSelection, Selection: app.TextRange{End: 2}}); err != nil {
		t.Fatal(err)
	}
	if w.ui.offset[1] != 60.5 {
		t.Fatalf("caret edit moved precise scroll to %v", w.ui.offset[1])
	}
	next, err := input.OnEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "中文", MarkedSelection: app.TextRange{End: 2}})
	if err != nil || !next.Composing {
		t.Fatalf("native preedit=%+v err=%v", next, err)
	}
	before := w.view.filterEditor.String()
	w.key(gfx.KeyEvent{Target: id, Key: gfx.KeyEscape})
	if w.view.filterEditor.String() != before || !w.view.filterOpen {
		t.Fatal("Escape destroyed composing text")
	}
	next, err = input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "配置"})
	if err != nil || next.Composing || len(w.view.rows) != 200 {
		t.Fatalf("IME commit=%+v rows=%d err=%v", next, len(w.view.rows), err)
	}
}

func TestSFTPWindowFilterLayoutVariants(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		for _, light := range []bool{false, true} {
			for _, width := range []int{640, 1200} {
				t.Run(fmt.Sprintf("%s-light-%t-width-%d", language, light, width), func(t *testing.T) {
					state, w, _, host, _ := dualSFTPFixture(t, 1.25)
					view := host.windows[0].view
					state.setLanguage(language)
					if light {
						state.setAppearance(appearanceLight)
					}
					if err := view.Resize(width, 600); err != nil {
						t.Fatal(err)
					}
					dualFind(t, view, w, true)
					dualReplaceFilter(t, view, "中文")
					dualFind(t, view, w, false)
					dualReplaceFilter(t, view, "note")
					for side := range 2 {
						filter := dualHit(t, view, sftpWindowFilter[side])
						more := dualHit(t, view, sftpWindowMore[side])
						list := w.ui.list[side]
						if filter.X < list.X-2 || filter.X+filter.Width > list.X+list.Width+2 || more.X < list.X-2 || more.X+more.Width > list.X+list.Width+2 {
							t.Fatalf("controls overflow pane %d: filter=%+v more=%+v list=%+v", side, filter, more, list)
						}
					}
					dualText(t, view, state.messages.SFTPWindow.Local)
					dualText(t, view, state.messages.SFTPWindow.Remote)
					dualText(t, view, fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, 1, 3))
					dualText(t, view, fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, 1, 2))
					dualClickID(t, view, sftpWindowMore[1])
					dualHit(t, view, sftpWindowHeaderHidden[1])
					if err := view.Rest(); err != nil {
						t.Fatal(err)
					}
					if dir := os.Getenv("MOTTY_SFTP_WINDOW_SCREENSHOT_DIR"); dir != "" {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("filters-%s-light-%t-width-%d.png", language, light, width))); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		}
	}
}

func BenchmarkSFTPWindowFilter(b *testing.B) {
	for _, count := range []int{1000, 20000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			state, session, _, _ := openTestSFTP(b)
			entries := make([]sftpclient.Entry, count)
			for i := range entries {
				entries[i].Name = fmt.Sprintf("配置文件-%05d.yml", i)
			}
			state.showSFTPListing(session.files, session.files.path, entries)
			folder := &session.files.sftpFolder
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(folder.filterInputEditor())
				value := "配置文件-0"
				if i%2 == 1 {
					value = "配置文件-1"
				}
				state.applySFTPFilterText(session.files, folder, app.TextEvent{Kind: app.TextInsert, Text: value})
			}
		})
	}
}

func TestSFTPWindowFilterShortcutUsesFocusedSideToolbar(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1)
	for side := range 2 {
		for _, id := range []gfx.ID{sftpWindowRefresh[side], sftpWindowUp[side], sftpWindowActions[side].At(0)} {
			w.activeLocal, w.view.local = side != 0, side != 0
			got := w.shortcut(gfx.KeyEvent{Target: id, Modifiers: keymap.Primary, Text: "f"})
			if !got.Handled || got.Focus != sftpWindowFilter[side] {
				t.Fatalf("toolbar %v opened wrong side filter: %+v", id, got)
			}
		}
	}
}

func TestSFTPPanelDefaultOrderingPreservesExistingListing(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.showSFTPListing(session.files, session.files.path, []sftpclient.Entry{{Name: "A.txt"}, {Name: "B.txt"}, {Name: "a.txt"}, {Name: "b.txt"}})
	if got := fmt.Sprint(rowNames(session.files.rows)); got != "[A.txt B.txt a.txt b.txt]" {
		t.Fatalf("new sort affordance changed default listing order: %s", got)
	}
}

func TestSFTPWindowNativeFindFromRemoteToolbarAfterTab(t *testing.T) {
	for _, target := range []gfx.ID{hitSFTPWindowUpload, hitSFTPWindowDownload, hitSFTPBookmarkList, hitSFTPBookmarkToggle} {
		t.Run(tree.Text(target), func(t *testing.T) {
			_, w, _, host, _ := dualSFTPFixture(t, 1)
			view := host.windows[0].view
			dualClickID(t, view, dualRow(t, w, false, "notes.txt"))
			dualClickID(t, view, dualRow(t, w, true, "中文上传.yml"))
			for steps := 0; view.Snapshot().Focused != target; steps++ {
				if steps >= 80 {
					t.Fatal("remote control not keyboard reachable")
				}
				if err := view.Driver().Key(gfx.KeyTab, 0); err != nil {
					t.Fatal(err)
				}
			}
			if !w.activeLocal {
				t.Fatal("fixture passed remote path editor instead of exercising toolbar focus")
			}
			if _, err := view.Driver().ShortcutChord(keymap.Primary, 'f'); err != nil {
				t.Fatal(err)
			}
			if view.Snapshot().Focused != sftpWindowFilter[1] || !w.view.filterOpen || w.view.lfolder.filterOpen {
				t.Fatalf("Tab-focused remote control opened wrong side filter: focus=%s local=%t", tree.Text(view.Snapshot().Focused), w.activeLocal)
			}
		})
	}
}
