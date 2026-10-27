package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func typeLinkField(t *testing.T, state *State, hit ui.ID, value string) {
	t.Helper()
	editor := state.EditorFor(hit)
	if editor == nil {
		t.Fatalf("hit %d has no editor", hit)
	}
	editor.Insert(value)
}

func TestNewSymbolicLinkIsCreatedListedAndOpensLikeAFolder(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPNewLink)
	if view.dialog != sftpDialogSymlink {
		t.Fatalf("dialog = %d", view.dialog)
	}
	if got := view.dialogEditor2.String(); got != "docs" {
		t.Fatalf("the target should start as the selected item, got %q", got)
	}
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogSymlink || view.dialogError != state.messages.Tools.LinkErrorName {
		t.Fatalf("an unnamed link was accepted: dialog %d, error %q", view.dialog, view.dialogError)
	}
	typeLinkField(t, state, hitSFTPDialogField, "shortcut")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && view.dialog == sftpDialogNone && len(view.rows) == 3
	})
	remote.mu.Lock()
	target := remote.links["/home/test/shortcut"]
	remote.mu.Unlock()
	if target != "docs" {
		t.Fatalf("link target = %q", target)
	}
	row := view.rows[sftpRowIndex(t, view, "shortcut")]
	if !row.entry.IsLink() || !row.entry.IsDir() || row.entry.LinkTarget != "docs" {
		t.Fatalf("link row = %+v", row.entry)
	}
	// It opens like the folder it points at.
	clickSFTPRow(t, state, view, "shortcut")
	state.Click(hitSFTPOpenEntry)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/shortcut" })
}

func TestConflictDialogCanSkipWhatExists(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	target := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(target, []byte("mine"), 0o600)
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return target, nil }
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.dialog == sftpDialogOverwrite })
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitSFTPDialogSkip); !ok {
		t.Fatal("the conflict dialog has no Skip button")
	}
	state.Click(hitSFTPDialogSkip)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.dialog == sftpDialogNone && len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	if content, _ := os.ReadFile(target); string(content) != "mine" {
		t.Fatalf("skipping overwrote the file: %q", content)
	}
}

func TestBookmarksAreKeptPerHostSurviveARestartAndOpenTheirFolder(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPOpenEntry)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	state.Click(hitSFTPBookmarkToggle)
	if marks := state.bookmarksFor(session); len(marks) != 1 || marks[0] != "/home/test/docs" {
		t.Fatalf("bookmarks = %v", marks)
	}
	// The star shows it.
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPBookmarkToggle)); !ok {
		t.Fatal("no bookmark star")
	}

	// Kept across a restart, for that host only.
	reopened := newTestState(t, state.options)
	if len(reopened.sftpBookmarks) != 1 || reopened.sftpBookmarks[0].host != session.host.CatalogID {
		t.Fatalf("restored bookmarks = %+v", reopened.sftpBookmarks)
	}

	// Going up and choosing it from the list returns to it.
	state.Click(hitSFTPUp)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
	state.Click(hitSFTPBookmarkList)
	if !view.bookmarksOpen {
		t.Fatal("the list did not open")
	}
	canvas.Reset()
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPBookmarkItemList.At(0))); !ok {
		t.Fatal("the list has no entry")
	}
	// The list is a popup above the file rows: its text is on a layer over theirs,
	// or the names of the rows under it show through.
	listed := false
	for _, text := range canvas.Texts() {
		if text.Value == "/home/test/docs" {
			listed = true
			if text.Layer != popupLayer {
				t.Fatalf("the bookmark's text is on layer %d, want the popup layer %d", text.Layer, popupLayer)
			}
		}
	}
	if !listed {
		t.Fatal("the bookmark is not painted in the list")
	}
	state.Click(hitSFTPBookmarkItemList.At(0))
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if view.bookmarksOpen {
		t.Fatal("the list stayed open after a choice")
	}

	// The star again removes it.
	state.Click(hitSFTPBookmarkToggle)
	if marks := state.bookmarksFor(session); len(marks) != 0 {
		t.Fatalf("bookmarks after removing = %v", marks)
	}
}

func TestBookmarksFromAFileAreCleanedAndBounded(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var saved savedState
	for index := 0; index < 60; index++ {
		saved.SFTPBookmarks = append(saved.SFTPBookmarks, savedBookmark{Host: "h", Path: "/p/" + strings.Repeat("x", index+1)})
	}
	saved.SFTPBookmarks = append(saved.SFTPBookmarks,
		savedBookmark{Host: "", Path: "/no-host"}, savedBookmark{Host: "h2", Path: "relative"}, savedBookmark{Host: "h2", Path: "/a/../b"},
		savedBookmark{Host: "h2", Path: "/b"}, savedBookmark{Host: "h2", Path: "/x\x00y"})
	state.restoreSFTPSettings(saved)
	perHost := map[string]int{}
	for _, mark := range state.sftpBookmarks {
		perHost[mark.host]++
	}
	if perHost["h"] != maxBookmarksPerHost || perHost["h2"] != 1 || perHost[""] != 0 {
		t.Fatalf("per host = %v", perHost)
	}
}

func TestTheFolderTheShellReportsMovesThePanelUnlessTurnedOff(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	view := session.files
	shell := fake.shell(t)
	shell.say("\x1b]7;file://box/home/test/docs\x07")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })

	state.Click(hitSettingsFollowToggle)
	if !state.sftpFollowOff {
		t.Fatal("following was not turned off")
	}
	state.openSFTPDirectory(view, "/home/test")
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
	shell.say("\x1b]7;file://box/home/test/docs\x1b\\")
	state.Wake()
	time.Sleep(30 * time.Millisecond)
	state.Wake()
	if view.path != "/home/test" {
		t.Fatalf("the panel followed though following is off: %s", view.path)
	}
}

func TestFilesSettingsApplyToTheWorkerAndAreSaved(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsFiles)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, id := range []ui.ID{hitSettingsParallel1, hitSettingsParallel4, hitSettingsKeepTimesToggle, hitSettingsFollowToggle, hitSettingsFollowSetup} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Errorf("settings control %d is not painted", id)
		}
	}
	var copied string
	state.options.SetClipboardText = func(value string) error { copied = value; return nil }
	state.Click(hitSettingsFollowSetup)
	if !strings.Contains(copied, "\\e]7;file://") {
		t.Fatalf("copied %q", copied)
	}
	state.Click(hitSettingsParallel4)
	state.Click(hitSettingsKeepTimesToggle)
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SFTPParallel != 4 || saved.SFTPKeepTimes != "off" || saved.SFTPFollow != "" {
		t.Fatalf("saved = %d %q %q", saved.SFTPParallel, saved.SFTPKeepTimes, saved.SFTPFollow)
	}
	restarted := newTestState(t, state.options)
	if restarted.sftpParallel != 4 || !restarted.sftpKeepTimesOff || restarted.sftpFollowOff {
		t.Fatalf("restored = %d %v %v", restarted.sftpParallel, restarted.sftpKeepTimesOff, restarted.sftpFollowOff)
	}
	_ = session
}

func TestExtractCommandsQuoteEverythingAndNeverOverwrite(t *testing.T) {
	command := extractCommand(archiveZip, "/srv/it's here/a b.zip", "/srv/it's here/a b")
	for _, want := range []string{`'/srv/it'\''s here/a b.zip'`, `mkdir -- '/srv/it'\''s here/a b'`, "unzip -q -n --", "exit 17"} {
		if !strings.Contains(command, want) {
			t.Errorf("command %q lacks %q", command, want)
		}
	}
	if got := extractCommand(archiveTar, "/x/y.tar.gz", "/x/y"); !strings.Contains(got, "tar -xf '/x/y.tar.gz'") {
		t.Errorf("tar command = %q", got)
	}
	if extractCommand(archiveNone, "/x", "/y") != "" {
		t.Error("a command for something that is not an archive")
	}
	for name, want := range map[string]archiveKind{"a.zip": archiveZip, "A.ZIP": archiveZip, "b.tar.gz": archiveTar, "c.TGZ": archiveTar, "d.tar.xz": archiveTar, "e.txt": archiveNone, "zip": archiveNone} {
		if got := archiveKindOf(name); got != want {
			t.Errorf("archiveKindOf(%q) = %d, want %d", name, got, want)
		}
	}
	for name, want := range map[string]string{"proj.tar.gz": "proj", "x.tgz": "x", "a.b.zip": "a.b", "plain": "plain"} {
		if got := archiveFolderName(name); got != want {
			t.Errorf("archiveFolderName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestExtractingAnArchiveRunsTheCommandAndRefreshes(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{name: "pack.zip", size: 10, mode: 0o644, when: time.Unix(1_700_000_300, 0)})
	remote.files["/home/test/pack.zip"] = []byte("zip bytes")
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 3 })

	var ran string
	fake.mu.Lock()
	fake.runCommand = func(_ context.Context, command string, _ int) ([]byte, error) {
		ran = command
		remote.mu.Lock()
		remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{name: "pack", mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_400, 0)})
		remote.mu.Unlock()
		return []byte("done"), nil
	}
	fake.mu.Unlock()

	clickSFTPRow(t, state, view, "pack.zip")
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPExtract)); !ok {
		t.Fatal("no Extract button for an archive")
	}
	state.Click(hitSFTPExtract)
	if view.dialog != sftpDialogExtract || view.dialogEditor.String() != "pack" {
		t.Fatalf("dialog %d, folder %q", view.dialog, view.dialogEditor.String())
	}
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && !view.extracting && len(view.rows) == 4 })
	if !strings.Contains(ran, "'/home/test/pack.zip'") || !strings.Contains(ran, "mkdir -- '/home/test/pack'") {
		t.Fatalf("ran %q", ran)
	}
	if !strings.Contains(view.status, "pack") || view.errorText != "" {
		t.Fatalf("status %q error %q", view.status, view.errorText)
	}
}

func TestAFailedExtractionShowsWhyAndAPlainFileOffersNone(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	view := session.files
	fake.mu.Lock()
	fake.runCommand = func(context.Context, string, int) ([]byte, error) {
		return []byte("already exists\n"), errors.New("exit status 17")
	}
	fake.mu.Unlock()
	view.dialogEntry = sftpclient.Entry{Name: "x.tar.gz"}
	view.dialogEditor.Insert("x")
	view.dialog = sftpDialogExtract
	state.submitSFTPExtract(view)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(state.fileNotices.items) > 0 })
	if view.errorText != "" || !strings.Contains(state.fileNotices.items[0].detail, "already exists") {
		t.Fatalf("inline error = %q, global errors = %+v", view.errorText, state.fileNotices.items)
	}
	clickSFTPRow(t, state, view, "notes.txt")
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, contextID(hitSFTPExtract)); ok {
		t.Fatal("Extract is offered for a text file")
	}
}

func TestTheSelectedItemsDetailsNameItsOwnerAndLinkTarget(t *testing.T) {
	state := newTestState(t, testOptions(t))
	line := state.sftpDetails(sftpclient.Entry{Name: "f", Size: 2048, Mode: 0o640, HasOwner: true, UID: 1000, GID: 100, Owner: "ann"})
	for _, want := range []string{"-rw-r-----", "ann:100", "2.0 KB"} {
		if !strings.Contains(line, want) {
			t.Errorf("details %q lack %q", line, want)
		}
	}
	link := state.sftpDetails(sftpclient.Entry{Name: "l", Mode: os.ModeSymlink | 0o777, LinkTarget: "gone", LinkBroken: true})
	if !strings.Contains(link, "→ gone") || !strings.Contains(link, state.messages.Tools.LinkBroken) {
		t.Errorf("link details = %q", link)
	}
}
