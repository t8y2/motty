package workbench

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/selfupdate"
	"motty/internal/sshconn"
	"motty/internal/update"
)

// fakeUpdater stands in for the real one, which would download and replace the
// test binary.
type fakeUpdater struct {
	mu       sync.Mutex
	reason   selfupdate.Reason
	prepare  func(ctx context.Context, progress func(selfupdate.Progress), phase func(selfupdate.Phase)) (selfupdate.Staged, error)
	launched int
	failure  string
}

func (updater *fakeUpdater) Support() selfupdate.Reason { return updater.reason }

func (updater *fakeUpdater) Prepare(ctx context.Context, _ update.Release, progress func(selfupdate.Progress), phase func(selfupdate.Phase)) (selfupdate.Staged, error) {
	if updater.prepare != nil {
		return updater.prepare(ctx, progress, phase)
	}
	return selfupdate.Staged{Version: "99.0.0"}, nil
}

func (updater *fakeUpdater) Launch(selfupdate.Staged) error {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	updater.launched++
	return nil
}

func (updater *fakeUpdater) TakeFailure() string {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	reason := updater.failure
	updater.failure = ""
	return reason
}

func (updater *fakeUpdater) launches() int {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	return updater.launched
}

var offeredRelease = update.Release{
	Version: "99.0.0", URL: "https://example.com/release", Notes: "- better",
	Asset: update.Asset{Name: "motty-99.0.0-x.zip", URL: "https://example.com/a", SHA256: strings.Repeat("a", 64), Size: 10, Signature: "c2ln"},
}

// updateWindow is a window that has been offered offeredRelease.
func updateWindow(t *testing.T, updater *fakeUpdater, quits *int, opened *[]string) *State {
	t.Helper()
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) { return offeredRelease, nil }
	options.Updater = updater
	options.Quit = func() { *quits++ }
	options.OpenURL = func(url string) error { *opened = append(*opened, url); return nil }
	state := newTestState(t, options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	return state
}

func TestAnUpdateThatCanBeInstalledIsInstalledAndTheApplicationQuits(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	updater.prepare = func(_ context.Context, progress func(selfupdate.Progress), phase func(selfupdate.Phase)) (selfupdate.Staged, error) {
		phase(selfupdate.PhaseDownloading)
		progress(selfupdate.Progress{Done: 5, Size: 10})
		phase(selfupdate.PhaseVerifying)
		return selfupdate.Staged{Version: "99.0.0"}, nil
	}
	state := updateWindow(t, updater, &quits, &opened)
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.UpdateInstall) || contains(texts, state.messages.Tools.GeneralUpdateDownload) {
		t.Fatalf("the dialog does not offer to install: %q", texts)
	}
	state.Click(hitUpdateInstall)
	waitUpdate(t, state, func() bool { return quits == 1 })
	if updater.launches() != 1 || state.update.install.phase != installRestarting || len(opened) != 0 {
		t.Fatalf("launched %d, phase %d, opened %q", updater.launches(), state.update.install.phase, opened)
	}
}

func TestARegisteredCustomWindowsInstallUsesTheInAppUpdater(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files", "Motty 自定义")
	install, reason := selfupdate.Detect(filepath.Join(dir, "motty.exe"), selfupdate.Env{
		GOOS: "windows", WindowsUserDir: dir,
		Exists:   func(path string) bool { return path == filepath.Join(dir, "Uninstall.exe") },
		Writable: func(path string) bool { return path == dir },
	})
	updater, quits, opened := &fakeUpdater{reason: reason}, 0, []string(nil)
	updater.prepare = func(context.Context, func(selfupdate.Progress), func(selfupdate.Phase)) (selfupdate.Staged, error) {
		return selfupdate.Staged{Install: install, Version: "99.0.0"}, nil
	}
	state := updateWindow(t, updater, &quits, &opened)
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.UpdateInstall) || contains(texts, state.messages.Tools.GeneralUpdateDownload) {
		t.Fatal("a registered custom installation still falls back to the browser")
	}
	state.Click(hitUpdateInstall)
	waitUpdate(t, state, func() bool { return quits == 1 })
	if updater.launches() != 1 || len(opened) != 0 {
		t.Fatalf("custom install launched %d times and opened %q", updater.launches(), opened)
	}
}

func TestAnUnsignedReleaseIsDownloadedByHand(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	state.update.release.Asset.Signature = ""
	_, texts := paintedTexts(state)
	m := state.messages.Tools
	if contains(texts, m.UpdateInstall) || !contains(texts, m.GeneralUpdateDownload) || !contains(texts, m.UpdateWhyUnsigned) {
		t.Fatalf("texts %q", texts)
	}
	state.Click(hitUpdateDownload)
	if !reflect.DeepEqual(opened, []string{"https://example.com/release"}) || quits != 0 {
		t.Fatalf("opened %q, quits %d", opened, quits)
	}
}

func TestAnUpdateThatCannotBeInstalledSaysWhyAndKeepsTheDownloadPage(t *testing.T) {
	updater, quits, opened := &fakeUpdater{reason: selfupdate.ReasonNotUserInstall}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	_, texts := paintedTexts(state)
	m := state.messages.Tools
	if contains(texts, m.UpdateInstall) || !contains(texts, m.GeneralUpdateDownload) || !contains(texts, m.UpdateWhyNotUserInstall) {
		t.Fatalf("texts %q", texts)
	}
	state.Click(hitUpdateDownload)
	if !reflect.DeepEqual(opened, []string{"https://example.com/release"}) || quits != 0 {
		t.Fatalf("opened %q, quits %d", opened, quits)
	}
}

func TestASystemInstalledCopyPointsAtThePackageManager(t *testing.T) {
	updater, quits, opened := &fakeUpdater{reason: selfupdate.ReasonSystemInstall}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	_, texts := paintedTexts(state)
	m := state.messages.Tools
	if contains(texts, m.UpdateInstall) || !strings.Contains(strings.Join(texts, " "), "installed system-wide") {
		t.Fatalf("texts %q", texts)
	}
}

func TestAnInstallThatFailsStaysOnTheDialogWithItsReasonAndTheReleasePage(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	updater.prepare = func(context.Context, func(selfupdate.Progress), func(selfupdate.Phase)) (selfupdate.Staged, error) {
		return selfupdate.Staged{}, errors.New("checksum differs")
	}
	state := updateWindow(t, updater, &quits, &opened)
	state.Click(hitUpdateInstall)
	waitUpdate(t, state, func() bool { return state.update.install.phase == installFailed })
	_, texts := paintedTexts(state)
	if !contains(texts, "checksum differs") || !contains(texts, state.messages.Tools.UpdateOpenPage) {
		t.Fatalf("texts %q", texts)
	}
	state.Click(hitUpdateProgressPage)
	if !reflect.DeepEqual(opened, []string{"https://example.com/release"}) || quits != 0 || updater.launches() != 0 {
		t.Fatalf("opened %q, quits %d, launches %d", opened, quits, updater.launches())
	}
	state.Click(hitUpdateProgressCancel)
	if state.modal != modalNone {
		t.Fatal("the failure could not be closed")
	}
}

func TestCancellingADownloadStopsItAndInstallsNothing(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	started, stopped := make(chan struct{}), make(chan struct{})
	updater.prepare = func(ctx context.Context, _ func(selfupdate.Progress), _ func(selfupdate.Phase)) (selfupdate.Staged, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return selfupdate.Staged{}, ctx.Err()
	}
	state := updateWindow(t, updater, &quits, &opened)
	state.Click(hitUpdateInstall)
	<-started
	state.Click(hitUpdateProgressCancel)
	<-stopped
	waitUpdate(t, state, func() bool { return true })
	if state.modal != modalNone || quits != 0 || updater.launches() != 0 {
		t.Fatalf("modal %d, quits %d, launches %d", state.modal, quits, updater.launches())
	}
}

func TestInstallingAsksBeforeDroppingOpenSessions(t *testing.T) {
	world, options := newWorld(t)
	options.Invalidate = func() {}
	updater, quits := &fakeUpdater{}, 0
	options.CheckUpdate = func() (update.Release, error) { return offeredRelease, nil }
	options.Updater, options.Quit = updater, func() { quits++ }
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	fake.shell(t).say("\x1b[0m")
	state.Wake()

	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	state.Click(hitUpdateInstall)
	if state.modal != modalForm || !strings.HasPrefix(state.form.description, "1 ") || updater.launches() != 0 {
		t.Fatalf("no question: modal %d, form %q", state.modal, state.form.description)
	}
	state.Click(hitFormCancel)
	if state.modal != modalUpdate {
		t.Fatalf("cancelling does not go back to the update: modal %d", state.modal)
	}
	state.Click(hitUpdateInstall)
	state.Click(hitFormSubmit)
	waitUpdate(t, state, func() bool { return quits == 1 })
}

func TestAnUpdateThatDidNotFinishIsReportedOnTheNextStart(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.Updater = &fakeUpdater{failure: "the installer failed"}
	state := newTestState(t, options)
	if state.modal != modalStateNotice {
		t.Fatalf("modal %d", state.modal)
	}
	_, texts := paintedTexts(state)
	if !contains(texts, "the installer failed") {
		t.Fatalf("texts %q", texts)
	}
}

func TestLongReleaseNotesScrollInsideABoundedArea(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	paintedTexts(state)
	notes := &state.update.notes
	if notes.scrolls || notes.viewHeight >= updateNotesMaxHeight {
		t.Fatalf("one short note should not scroll or fill the area: %+v", notes)
	}

	var long strings.Builder
	for range 50 {
		long.WriteString("- an entry of the changelog\n")
	}
	state.update.notes = updateNotes{text: long.String()}
	paintedTexts(state)
	if !notes.scrolls || notes.viewHeight != updateNotesMaxHeight || notes.max <= 0 {
		t.Fatalf("fifty notes should fill the cap and scroll: %+v", notes)
	}
	result := state.Scroll(ui.ScrollEvent{DeltaY: 100000, Precise: true})
	if !result.Handled || notes.scroll != notes.max {
		t.Fatalf("the wheel did not reach the end of the notes: scroll %v, max %v", notes.scroll, notes.max)
	}
	state.Key(ui.KeyEvent{Key: ui.KeyHome, Target: hitUpdateNotes})
	if notes.scroll != 0 {
		t.Fatalf("Home did not return to the top: %v", notes.scroll)
	}
}

func TestUpdateNotesScrollbarCanBeDragged(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	var long strings.Builder
	for range 50 {
		long.WriteString("- an entry of the changelog\n")
	}
	state.update.notes = updateNotes{text: long.String()}
	canvas, _ := paintedTexts(state)

	notes := &state.update.notes
	bar := notes.bar
	if !notes.scrolls || bar.MaxOffset <= 0 || bar.Thumb.Height <= 0 {
		t.Fatalf("notes did not produce a draggable scrollbar: %+v", bar)
	}
	foundBar := false
	for _, hit := range canvas.Hits() {
		if hit.ID == hitUpdateNotesScrollbar && hit.Role == ui.RoleSlider {
			foundBar = true
			break
		}
	}
	if !foundBar {
		t.Fatalf("notes scrollbar did not declare slider semantics: %+v", canvas.Hits())
	}

	downY := bar.Thumb.Y + bar.Thumb.Height/2
	down := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerDown, Button: ui.PointerButtonPrimary,
		Target: hitUpdateNotesScrollbar, X: bar.Track.X + bar.Track.Width/2, Y: downY,
		Viewport: state.viewport,
	})
	if !down.Handled || !notes.dragging {
		t.Fatalf("pressing the thumb did not start a drag: %+v, notes %+v", down, notes)
	}

	travel := bar.Track.Height - bar.Thumb.Height
	moveY := downY + travel*0.75
	move := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerMove, Button: ui.PointerButtonPrimary, Captured: true,
		Target: hitUpdateNotesScrollbar, X: bar.Track.X + bar.Track.Width/2, Y: moveY,
		Viewport: state.viewport,
	})
	if !move.Handled || !move.Repaint || notes.scroll < bar.MaxOffset*0.70 || notes.scroll > bar.MaxOffset*0.80 {
		t.Fatalf("dragging moved notes to %v of %v: %+v", notes.scroll, bar.MaxOffset, move)
	}

	up := state.Pointer(ui.PointerEvent{
		Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Captured: true,
		Target: hitUpdateNotesScrollbar, X: bar.Track.X + bar.Track.Width/2, Y: moveY,
		Viewport: state.viewport,
	})
	if !up.Handled || notes.dragging {
		t.Fatalf("releasing the thumb did not end the drag: %+v, notes %+v", up, notes)
	}

	state.Key(ui.KeyEvent{Key: ui.KeyHome, Target: hitUpdateNotesScrollbar})
	if notes.scroll != 0 {
		t.Fatalf("Home with the scrollbar focused did not return to the top: %v", notes.scroll)
	}
}

func TestUpdateNotesDragDoesNotSurviveReopen(t *testing.T) {
	updater, quits, opened := &fakeUpdater{}, 0, []string(nil)
	state := updateWindow(t, updater, &quits, &opened)
	notes := &state.update.notes
	notes.dragging = true

	state.closeModal()
	state.update.offer = true
	state.showNextPrompt()

	if state.modal != modalUpdate || notes.dragging {
		t.Fatalf("reopened update dialog retained an old drag: modal %v, notes %+v", state.modal, notes)
	}
}
