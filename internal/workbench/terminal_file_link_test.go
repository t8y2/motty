package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

// terminalCell is the row and column where text first appears on the session's
// screen.
func terminalCell(t *testing.T, session *Session, text string) (int, int) {
	t.Helper()
	want := []rune(text)
	_, rows := session.term.screen.Size()
	for row := range rows {
		var runes []rune
		var columns []int
		for column, cell := range session.term.screen.Row(row).Cells {
			if cell.R != 0 && cell.Attr&term.WideTail == 0 {
				runes, columns = append(runes, cell.R), append(columns, column)
			}
		}
		for index := 0; index+len(want) <= len(runes); index++ {
			if slices.Equal(runes[index:index+len(want)], want) {
				return row, columns[index]
			}
		}
	}
	t.Fatalf("%q is not on the screen", text)
	return 0, 0
}

// pressTerminalCell presses a button on a cell of the session's terminal, with
// modifiers held, and releases the primary one there. The events go to the
// terminal itself, in the window's frame, as the panes deliver them.
func pressTerminalCell(state *State, session *Session, row, column int, button ui.PointerButton, modifiers ui.Modifiers) {
	size := ui.Size{Width: 1400, Height: 900}
	geometry := state.terminalLayoutForEvent(session.ID, size)
	x, y := cellPoint(geometry, row, column, state.cellWidth, state.cellHeight)
	hit := hitTerminalList.At(int(session.ID))
	state.terminalPointer(session, ui.PointerEvent{Kind: ui.PointerDown, Button: button, Modifiers: modifiers, Target: hit, X: x, Y: y, Viewport: size})
	if button == ui.PointerButtonPrimary {
		state.terminalPointer(session, ui.PointerEvent{Kind: ui.PointerUp, Button: button, Modifiers: modifiers, Target: hit, Captured: true, X: x, Y: y, Viewport: size})
	}
}

func selectedSFTPName(view *sftpView) string {
	if view.selected < 0 || view.selected >= len(view.rows) || !view.rows[view.selected].selected {
		return ""
	}
	return view.rows[view.selected].entry.Name
}

func TestAFileLinkNamesAPathOnTheMachineTheSessionRunsOn(t *testing.T) {
	_, session, _, _ := openTestSFTP(t)
	for _, test := range []struct {
		target, want string
	}{
		{"file:///home/test/docs/guide.md", "/home/test/docs/guide.md"},
		{"file://build-01/home/test/World%20Models%20总结.md", "/home/test/World Models 总结.md"},
		{"vscode://file/home/test/main.go:42:7", "/home/test/main.go"},
		{"cursor://file/home/test/docs/", "/home/test/docs"},
		{"/home/test/notes.txt", "/home/test/notes.txt"},
		{"https://example.test/docs", ""},
		{"vscode://extension/some.extension", ""},
		{"file:relative.md", ""},
		{"file:///home/test/bad\x07name", ""},
	} {
		got, ok := fileLinkTarget(session, test.target)
		if !ok {
			got = ""
		}
		if got != test.want {
			t.Errorf("fileLinkTarget(%q) = %q, want %q", test.target, got, test.want)
		}
	}

	local := &Session{HostID: localHostID}
	if _, ok := fileLinkTarget(local, "file://another-machine.example/home/test/a.md"); ok {
		t.Error("a local terminal took a file on another machine")
	}
	if runtime.GOOS == "windows" {
		if got, ok := fileLinkTarget(local, "file:///C:/Users/test/a%20b.md"); !ok || got != `C:\Users\test\a b.md` {
			t.Errorf("a Windows file URL = %q, %v", got, ok)
		}
	}
}

func TestCtrlClickOnAFileLinkSelectsTheFileInTheFilePanel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.terminalLinkPlainClick = false
	session.term.write([]byte("wrote \x1b]8;;file:///home/test/docs/guide.md\x1b\\the guide\x1b]8;;\x1b\\ for you\r\n"))
	row, column := terminalCell(t, session, "the guide")

	// A plain click is the terminal's own, as it is for every link.
	pressTerminalCell(state, session, row, column+2, ui.PointerButtonPrimary, 0)
	if view.path != "/home/test" || view.busy {
		t.Fatalf("a plain click moved the panel to %q", view.path)
	}

	status := state.status
	pressTerminalCell(state, session, row, column+2, ui.PointerButtonPrimary, defaultShortcutModifier())
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if name := selectedSFTPName(view); name != "guide.md" {
		t.Fatalf("selected %q in %v, want guide.md", name, rowNames(view.rows))
	}
	if state.status != status {
		t.Fatalf("the link also went to the system's opener: %q", state.status)
	}
}

func TestCtrlClickOnAFileLinkOpensTheClosedPanelOnTheServersFiles(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	openLocalPane(t, state, session, dir)
	state.hideFilesPanel(session)
	session.term.write([]byte("\x1b]8;;vscode://file/home/test/notes.txt:3\x1b\\notes.txt:3\x1b]8;;\x1b\\\r\n"))
	row, column := terminalCell(t, session, "notes.txt:3")

	pressTerminalCell(state, session, row, column, ui.PointerButtonPrimary, defaultShortcutModifier())
	if !state.filesPanelShown() {
		t.Fatal("the file panel stayed closed")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.local && !view.busy && selectedSFTPName(view) == "notes.txt" })
	if view := session.files; view.path != "/home/test" {
		t.Fatalf("the panel shows %q", view.path)
	}
}

func TestCtrlClickOnAnUnderlinedFileNameFindsItFromTheShellsFolder(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.entries["/home/test/docs"] = append(remote.entries["/home/test/docs"], workbenchFileInfo{name: "World Models 总结.md", mode: 0o644})
	remote.mu.Unlock()

	// An agent that makes no real link underlines the name of what it wrote,
	// relative to the folder it runs in.
	session.term.write([]byte("\x1b]7;file://server/home/test/docs\x07"))
	session.term.write([]byte("Saved \x1b[4;34mWorld Models 总结.md\x1b[0m. Open it.\r\n"))
	row, column := terminalCell(t, session, "World Models")
	pressTerminalCell(state, session, row, column+8, ui.PointerButtonPrimary, defaultShortcutModifier())
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if name := selectedSFTPName(view); name != "World Models 总结.md" {
		t.Fatalf("selected %q in %v", name, rowNames(view.rows))
	}

	// A name that is not in that folder is looked for in the folders below it.
	session.term.write([]byte("\x1b]7;file://server/home/test\x07"))
	session.term.write([]byte("See \x1b[4;34mguide.md\x1b[0m\r\n"))
	row, column = terminalCell(t, session, "guide.md")
	pressTerminalCell(state, session, row, column, ui.PointerButtonPrimary, defaultShortcutModifier())
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
	search := waitSearchDone(t, state, session, false)
	if got := searchResultPaths(search); !slices.Equal(got, []string{"docs/guide.md"}) || view.sftpFolder.filterEditor.String() != "guide.md" {
		t.Fatalf("searched %q and found %q", view.sftpFolder.filterEditor.String(), got)
	}

	// Underlined text that is no file name is left alone.
	before := view.path
	session.term.write([]byte("\x1b[4mimportant\x1b[0m\r\n"))
	row, column = terminalCell(t, session, "important")
	pressTerminalCell(state, session, row, column, ui.PointerButtonPrimary, defaultShortcutModifier())
	if view.path != before || view.busy {
		t.Fatalf("a plain underlined word moved the panel to %q", view.path)
	}
}

func TestTheMenuOnAFileLinkShowsOrDownloadsTheFile(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	downloads := t.TempDir()
	state.sftpDownloadDirectory = downloads
	state.terminalRightClickPaste = false
	session.term.write([]byte("\x1b]8;;file:///home/test/docs/guide.md\x1b\\guide\x1b]8;;\x1b\\\r\n"))
	row, column := terminalCell(t, session, "guide")

	pressTerminalCell(state, session, row, column, ui.PointerButtonSecondary, 0)
	menu := state.terminalMenu
	var kinds []actionKind
	for _, item := range menu.found[:menu.count] {
		kinds = append(kinds, item.kind)
	}
	if !menu.open || !slices.Equal(kinds, []actionKind{actionFiles, actionDownload, actionCopy}) {
		t.Fatalf("the menu offers %v (open %v)", kinds, menu.open)
	}
	if label := state.actionLabel(menu.found[1]); label != "Download guide.md" {
		t.Fatalf("download row = %q", label)
	}

	state.Click(hitTerminalMenuAction[1])
	local := filepath.Join(downloads, "guide.md")
	waitSFTP(t, state, session, func(view *sftpView) bool {
		data, err := os.ReadFile(local)
		return err == nil && string(data) == "guide\n"
	})
	if view := session.files; view.path != "/home/test/docs" || selectedSFTPName(view) != "guide.md" {
		t.Fatalf("the panel shows %q with %q selected", view.path, selectedSFTPName(view))
	}
}

func TestShowingAPathThatIsNotThereSaysSo(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.showInFiles(session, "/home/test/docs/gone.md", sftpReveal{})
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	view := session.files
	if want := "gone.md is not in this folder"; view.status != want || selectedSFTPName(view) != "" {
		t.Fatalf("status %q, selected %q", view.status, selectedSFTPName(view))
	}
	// The folder a path names with a slash at its end is opened, not selected.
	state.showInFiles(session, "/home/test/docs/", sftpReveal{})
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if view.path != "/home/test/docs" {
		t.Fatalf("the panel shows %q", view.path)
	}
}
