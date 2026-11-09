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
	// Launch starts what replaces the program once it has quit.
	Launch(staged selfupdate.Staged) error
	// TakeFailure is why the last update did not finish, if it did not, and forgets it.
	TakeFailure() string
}

type installPhase uint8

const (
	installDownloading installPhase = iota
	installVerifying
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

// startInstall downloads and checks the release on a goroutine, and hands over to
// the new version when that is done. Everything the goroutine reports goes
// through aiPost, so the state is only touched on the window's thread.
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
		state.aiPost(func(state *State) { state.finishInstall(serial, staged, err) })
	}()
}

func (state *State) finishInstall(serial int, staged selfupdate.Staged, err error) {
	install := &state.update.install
	if install.serial != serial || install.cancel == nil {
		return // cancelled, or the window went away
	}
	install.cancel()
	install.cancel = nil
	if errors.Is(err, context.Canceled) {
		return
	}
	if err == nil {
		err = state.options.Updater.Launch(staged)
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
		} else if install.phase != installRestarting {
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

func (state *State) paintUpdateProgress(canvas *gfx.Canvas, viewport gfx.Rect) {
	install, m, pal := &state.update.install, state.messages.Tools, state.pal
	title := fmt.Sprintf(m.UpdateDownloadingTitleFmt, state.update.release.Version)
	bodyHeight := float32(68)
	switch install.phase {
	case installRestarting:
		title = m.UpdateRestarting
		bodyHeight = 40
	case installFailed:
		title = m.UpdateFailedTitle
		bodyHeight = 4 * 20
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitUpdateProgressDimiss, CloseID: hitUpdateProgressCancel, InitialFocusID: hitUpdateProgressCancel,
		ContentSize: gfx.Size{Width: 540}, FitBody: true, BodyHeight: bodyHeight,
	})
	layout := dialog.Begin(title, "")
	switch install.phase {
	case installFailed:
		f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
		f.Paragraph(fmt.Sprintf(m.UpdateFailedBodyFmt, install.failure), 4).FontSize(metrics.FontSizeS).Color(pal.danger).LineHeight(20).MarginSides(4, 0, 0, 0)
		f.End()
		dialog.Actions(layout,
			components.ButtonAction{ID: hitUpdateProgressCancel, Label: state.messages.LabelWindowClose, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitUpdateProgressPage, Label: m.UpdateOpenPage})
	default:
		line := ""
		fraction := float32(1)
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
		}
		var bar ui.Box
		f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
		f.Column().Fill().Children(func() {
			bar = f.Slot().Height(8).Shrink(0).MarginSides(12, 0, 0, 0)
			f.Text(line).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).MarginSides(10, 0, 0, 0)
		})
		f.End()
		components.Progress(canvas, state.Theme(), bar.Rect(), fraction)
		if install.phase == installRestarting {
			dialog.End()
			return
		}
		dialog.Actions(layout, components.ButtonAction{ID: hitUpdateProgressCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary})
	}
	dialog.End()
}
