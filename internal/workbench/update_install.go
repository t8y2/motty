package workbench

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/selfupdate"
	"motty/internal/update"
)

// Updater replaces the running program with a release. selfupdate.Updater is
// the real one; the window needs only this, so a test can stand in for it.
type Updater interface {
	// Support says whether this installation can update itself, and if not why.
	Support() selfupdate.Reason
	Package() update.Package
	// Prepare downloads the release's package and checks it; it changes nothing.
	Prepare(ctx context.Context, release update.Release, progress func(selfupdate.Progress), phase func(selfupdate.Phase)) (selfupdate.Staged, error)
	// Launch stages the release and starts the helper that replaces the program
	// once this process has quit. It copies the new application and checks its
	// signature, so it must run off the window's thread; quit is called when the
	// helper is running, and the caller quits as soon as Launch returns.
	Launch(staged selfupdate.Staged, quit func()) error
	// TakeFailure is why the last update did not finish, if it did not, and forgets it.
	TakeFailure() string
}

type installPhase uint8

const (
	installDownloading installPhase = iota
	// installVerifying covers the checks and the work that turns the download
	// into an application: applying a delta patch, or unpacking and checking
	// the archive.
	installVerifying
	// installInstalling is the step that copies the new application beside the
	// installation and starts the helper replacing it. Nothing after this can
	// be cancelled: the helper is about to run.
	installInstalling
	installRestarting
	installFailed
)

// updateInstall is one attempt to install the offered release.
type updateInstall struct {
	phase    installPhase
	progress selfupdate.Progress
	// failure is what to show the user: the system's refusal is explained
	// rather than quoted, since the same sentence covers a missing ticket and
	// a machine that cannot reach Apple.
	failure string
	err     error
	cancel  context.CancelFunc
	// serial tells an answer from a cancelled attempt from the current one.
	serial int
	text   struct {
		done, size int64
		line       string
	}
}

var (
	hitUpdateInstall        = tree.ID(launchHits, "update-install")
	hitUpdateProgressDimiss = tree.ID(launchHits, "update-progress-dismiss")
	hitUpdateProgressCancel = tree.ID(launchHits, "update-progress-cancel")
	hitUpdateProgressPage   = tree.ID(launchHits, "update-progress-page")
)

// canInstallUpdate reports whether the offered release can replace this program.
func (state *State) canInstallUpdate() bool {
	return state.update.available && state.options.Updater != nil && state.options.Quit != nil &&
		state.update.release.Asset.Has() && state.update.release.Asset.Signed() &&
		state.options.Updater.Support() == selfupdate.ReasonNone
}

// whyNoInstall says in words why the offered release has to be downloaded by
// hand, or "" when nothing needs saying (it can be installed, or this build has
// no updater at all).
func (state *State) whyNoInstall() string {
	if state.options.Updater == nil || state.options.Quit == nil {
		return ""
	}
	m := state.messages.Tools
	switch state.options.Updater.Support() {
	case selfupdate.ReasonNotUserInstall:
		return m.UpdateWhyNotUserInstall
	case selfupdate.ReasonNotWritable:
		return m.UpdateWhyNotWritable
	case selfupdate.ReasonTranslocated:
		return m.UpdateWhyTranslocated
	case selfupdate.ReasonSystemInstall:
		return m.UpdateWhySystemInstall
	case selfupdate.ReasonAuthorizationUnavailable:
		return m.UpdateWhyNoPolkit
	}
	if !state.update.release.Asset.Has() {
		if state.update.release.PlatformPackageChecked {
			return m.UpdateWhyNoPackage
		}
		return m.UpdateWhyPackageUnknown
	}
	if !state.update.release.Asset.Signed() {
		return m.UpdateWhyUnsigned
	}
	return ""
}

// askInstall starts the installation, after asking when sessions would be lost.
func (state *State) askInstall() {
	count := 0
	if state.options.Group != nil {
		count = state.options.Group.sessionCount()
	} else {
		count = len(state.sessions)
	}
	if count == 0 {
		state.startInstall()
		return
	}
	m := state.messages.Tools
	state.openForm(formDialog{
		title: m.UpdateConfirmTitle, description: fmt.Sprintf(m.UpdateConfirmDescriptionFmt, count),
		submit: m.UpdateConfirmSubmit, danger: true,
		run: func(state *State, _ *formDialog) string {
			state.startInstall()
			return ""
		},
		cancel: func(state *State) { state.modal = modalUpdate },
	})
}

// startInstall downloads and checks the release on a goroutine, then hands it to
// the installer, which replaces the application once this process quits.
// Everything the goroutine reports goes through aiPost, so the state is only
// touched on the window's thread.
func (state *State) startInstall() {
	install := &state.update.install
	serial := install.serial + 1
	*install = updateInstall{serial: serial}
	ctx, cancel := context.WithCancel(context.Background())
	install.cancel = cancel
	state.modal = modalUpdateProgress
	release, updater := state.update.release, state.options.Updater
	go func() {
		staged, err := updater.Prepare(ctx, release,
			func(progress selfupdate.Progress) {
				state.aiPost(func(state *State) {
					if install := &state.update.install; install.serial == serial {
						install.progress = progress
					}
				})
			},
			func(phase selfupdate.Phase) {
				state.aiPost(func(state *State) {
					if install := &state.update.install; install.serial == serial && install.phase != installFailed {
						install.phase = installDownloading
						if phase == selfupdate.PhaseVerifying {
							install.phase = installVerifying
						}
					}
				})
			})
		if err == nil && ctx.Err() == nil {
			state.aiPost(func(state *State) { state.installStaged(serial, staged) })
			return
		}
		state.aiPost(func(state *State) { state.finishInstall(serial, err) })
	}()
}

// installStaged starts the installer on its own goroutine, so the window keeps
// drawing the step instead of freezing on the last frame, and so the reported
// step is the one actually running. It runs on the window's thread, which is
// also where a cancel would arrive: a cancel that lost the race is impossible
// once this returns, because the dialog stops offering one.
func (state *State) installStaged(serial int, staged selfupdate.Staged) {
	install := &state.update.install
	if install.serial != serial || install.cancel == nil || install.phase == installFailed {
		return // cancelled while the release was being prepared
	}
	install.phase = installInstalling
	updater := state.options.Updater
	go func() {
		// The installer asks for the application to quit once the helper that
		// replaces it is running. The quit is finishInstall's, on the window's
		// thread, and Launch returns right after the helper starts, so the
		// application still quits right after that request.
		err := updater.Launch(staged, func() {})
		state.aiPost(func(state *State) { state.finishInstall(serial, err) })
	}()
}

// finishInstall ends an attempt that has installed nothing yet, or one whose
// installer has finished: it saves the state and quits so the replacement can
// run.
func (state *State) finishInstall(serial int, err error) {
	install := &state.update.install
	if install.serial != serial || install.cancel == nil {
		return // cancelled, or the window went away
	}
	install.cancel()
	install.cancel = nil
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		install.phase, install.err = installFailed, err
		install.failure = strings.TrimPrefix(err.Error(), "selfupdate: ")
		if errors.Is(err, selfupdate.ErrNotAccepted) {
			install.failure = state.messages.Tools.UpdateBlockedBySystem
		}
		return
	}
	install.phase = installRestarting
	state.save()
	state.options.Quit()
}

// cancelInstall stops a download and closes its dialog.
func (state *State) cancelInstall() {
	install := &state.update.install
	if install.cancel != nil {
		install.cancel()
		install.cancel = nil
	}
	install.serial++
	state.closeModal()
	state.showNextPrompt()
}

func (state *State) clickUpdateProgress(id gfx.ID) {
	install := &state.update.install
	switch id {
	case hitUpdateProgressCancel:
		if install.phase == installFailed {
			state.closeModal()
			state.showNextPrompt()
		} else if install.phase != installRestarting && install.phase != installInstalling {
			state.cancelInstall()
		}
	case hitUpdateProgressDimiss:
		if install.phase == installFailed {
			state.closeModal()
			state.showNextPrompt()
		}
	case hitUpdateProgressPage:
		if install.phase == installFailed {
			state.openExternal(state.update.release.URL)
		}
	}
}

func (state *State) declareUpdateProgress(c ui.C) {
	install, m, pal := &state.update.install, state.messages.Tools, state.pal
	title := fmt.Sprintf(m.UpdateDownloadingTitleFmt, state.update.release.Version)
	bodyHeight := float32(68)
	switch install.phase {
	case installVerifying, installInstalling:
		// The download is done: what is left is turning it into the new
		// application, which the title should say rather than "downloading".
		title = fmt.Sprintf(m.UpdateInstallingTitleFmt, state.update.release.Version)
	case installRestarting:
		title = m.UpdateRestarting
		bodyHeight = 40
	case installFailed:
		title = m.UpdateFailedTitle
		bodyHeight = 4 * 20
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 540, "", bodyHeight)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 540, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitUpdateProgressDimiss, CloseID: hitUpdateProgressCancel,
		InitialFocusID: hitUpdateProgressCancel,
	})
	if install.phase == installFailed {
		dialog.Body(func() {
			ui.Column(c).Fill().Children(func() {
				ui.Paragraph(c, fmt.Sprintf(m.UpdateFailedBodyFmt, install.failure), 4).FontSize(metrics.FontSizeS).Color(pal.danger).LineHeight(20).MarginSides(4, 0, 0, 0)
			})
		})
		dialog.Footer(func() {
			state.dialogActionFooter(c,
				components.ButtonAction{ID: hitUpdateProgressCancel, Label: state.messages.LabelWindowClose, Variant: components.ButtonSecondary},
				components.ButtonAction{ID: hitUpdateProgressPage, Label: m.UpdateOpenPage},
			)
		})
	} else {
		line, fraction := "", float32(1)
		switch install.phase {
		case installDownloading:
			progress := install.progress
			if install.text.done != progress.Done || install.text.size != progress.Size {
				install.text.done, install.text.size = progress.Done, progress.Size
				install.text.line = formatBytes(progress.Done)
				if progress.Size > 0 {
					install.text.line += " / " + formatBytes(progress.Size)
				}
			}
			line = install.text.line
			fraction = 0
			if progress.Size > 0 {
				fraction = float32(float64(progress.Done) / float64(progress.Size))
			}
		case installVerifying:
			line = m.UpdateVerifying
		case installInstalling:
			line = m.UpdateInstalling
		}
		dialog.Body(func() {
			ui.Column(c).Fill().Children(func() {
				ui.Progress(c, fraction).Height(8).Shrink(0).FillWidth().MarginSides(12, 0, 0, 0).
					Label(title)
				ui.Text(c, line).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).MarginSides(10, 0, 0, 0)
			})
		})
		if install.phase != installRestarting {
			action := components.ButtonAction{ID: hitUpdateProgressCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary}
			// The installer is replacing the application either way once it has
			// started, so the button stays where it was and stops offering what it
			// cannot do.
			action.Disabled = install.phase == installInstalling
			dialog.Footer(func() {
				state.dialogActionFooter(c, action)
			})
		}
	}
}
