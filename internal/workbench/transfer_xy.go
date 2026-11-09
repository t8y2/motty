package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/xymodem"
	"motty/internal/zmodem"
)

// XMODEM and YMODEM. Neither announces itself in the stream, so the user starts
// them from the terminal's menu, says which protocol and which way, and has
// started the program on the other side. From then on it is the same transfer as
// a ZMODEM one: the dialog with its progress, the stream taken from the screen,
// the same cancel and quiet handling.

// transferXyHits is the place of the controls this file paints.
var transferXyHits = tree.Feature("transfer-xy")

var (
	hitXYStart   = tree.ID(transferXyHits, "xy-start")
	hitXYCancel  = tree.ID(transferXyHits, "xy-cancel")
	hitXYClose   = tree.ID(transferXyHits, "xy-close")
	hitXYDismiss = tree.ID(transferXyHits, "xy-dismiss")
)

// The dialog's two segmented choices. Each is a page of its own, declaring one
// Choice at the rectangle the dialog's body laid out.
var (
	xyModeChoices      = [...]xymodem.Mode{xymodem.XMODEM, xymodem.XMODEM1K, xymodem.YMODEM}
	xyDirectionChoices = [...]bool{true, false}
)

// xySetup is the dialog's choices.
type xySetup struct {
	mode    xymodem.Mode
	sending bool
	// modeChoice and directionChoice are the dialog's segmented controls, each
	// declared as a page so the dialog's keys can rove them.
	modeChoice, directionChoice dialogChoice
}

// xyModeChoiceLabel names one transfer mode as the dialog shows it.
func (state *State) xyModeChoiceLabel(mode xymodem.Mode) string {
	switch mode {
	case xymodem.XMODEM1K:
		return "XMODEM-1K"
	case xymodem.YMODEM:
		return "YMODEM"
	default:
		return "XMODEM"
	}
}

// xyDirectionChoiceLabel names the direction the dialog shows.
func (state *State) xyDirectionChoiceLabel(sending bool) string {
	if sending {
		return state.messages.Tools.XYSend
	}
	return state.messages.Tools.XYReceive
}

// xyChoiceKey gives the dialog's page choices their arrows before the rest of
// the window takes the key.
func (state *State) xyChoiceKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	for _, choice := range [...]*ui.Page{state.xy.modeChoice.page, state.xy.directionChoice.page} {
		if choice == nil {
			continue
		}
		if result, handled := choice.Key(event); handled {
			return result, true
		}
	}
	return gfx.EventResult{}, false
}

// declareXYModeChoice is the page of the transfer mode choice.
func (state *State) declareXYModeChoice(c ui.C) {
	ui.Choice(c, &state.xy.mode, xyModeChoices[:], state.xyModeChoiceLabel).Fill()
}

// declareXYDirectionChoice is the page of the send/receive choice.
func (state *State) declareXYDirectionChoice(c ui.C) {
	ui.Choice(c, &state.xy.sending, xyDirectionChoices[:], state.xyDirectionChoiceLabel).Fill()
}

// openXYSetup opens the dialog for the session.
func (state *State) openXYSetup(session *Session) {
	if session.term == nil || session.term.shell == nil || state.transfer.phase != transferNone {
		return
	}
	state.xy = xySetup{mode: xymodem.YMODEM, sending: true}
	state.modal, state.modalSession = modalXYTransfer, session.ID
}

func (state *State) clickXYSetup(id gfx.ID) {
	switch id {
	case hitXYStart:
		state.startXYTransfer()
	case hitXYCancel, hitXYClose, hitXYDismiss:
		state.closeModal()
	}
}

// startXYTransfer asks for the files or the place to put them, and starts.
func (state *State) startXYTransfer() {
	session := state.promptSession()
	if session == nil || session.term == nil || session.term.shell == nil || state.transfer.phase != transferNone {
		state.closeModal()
		return
	}
	m, setup := state.messages.Tools, state.xy
	var engine zmodem.Engine
	var target *folderTarget
	var sources []*os.File
	if setup.sending {
		paths, err := state.options.OpenFiles(app.OpenFileOptions{Title: m.TransferChooseFilesTitle, AllowMultiple: setup.mode == xymodem.YMODEM})
		if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
			return
		}
		if err != nil {
			state.status = err.Error()
			return
		}
		files, opened, message := state.openSendFiles(paths)
		if message != "" {
			state.status = message
			return
		}
		sources, engine = opened, xymodem.NewSender(setup.mode, files)
	} else if setup.mode == xymodem.YMODEM {
		dir, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: m.TransferChooseFolderTitle})
		if errors.Is(err, app.ErrCanceled) || err == nil && dir == "" {
			return
		}
		if err != nil {
			state.status = err.Error()
			return
		}
		target = &folderTarget{dir: dir}
		engine = xymodem.NewReceiver(setup.mode, "", target)
	} else {
		name, err := state.options.SaveFile(app.SaveFileOptions{Title: m.XYSaveTitle, SuggestedName: "received.bin"})
		if errors.Is(err, app.ErrCanceled) || err == nil && name == "" {
			return
		}
		if err != nil {
			state.status = err.Error()
			return
		}
		target = &folderTarget{dir: filepath.Dir(name), exact: filepath.Base(name)}
		engine = xymodem.NewReceiver(setup.mode, filepath.Base(name), target)
	}
	state.closeModal()
	state.transfer = fileTransfer{phase: transferOffered, sessionID: session.ID, sending: setup.sending, target: target, sources: sources}
	state.modal, state.modalSession = modalFileTransfer, session.ID
	state.startTransfer(session, engine)
}

// openSendFiles opens the files to send, refusing what a transfer cannot carry.
func (state *State) openSendFiles(paths []string) (files []zmodem.SendFile, opened []*os.File, message string) {
	m := state.messages.Tools
	closeAll := func() {
		for _, file := range opened {
			file.Close()
		}
		files, opened = nil, nil
	}
	if len(paths) > maxSendFiles {
		return nil, nil, fmt.Sprintf(m.TransferTooManyFmt, maxSendFiles)
	}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			closeAll()
			return nil, nil, err.Error()
		}
		opened = append(opened, file)
		info, err := file.Stat()
		switch {
		case err != nil:
			closeAll()
			return nil, nil, err.Error()
		case !info.Mode().IsRegular():
			closeAll()
			return nil, nil, fmt.Sprintf(m.TransferNotFileFmt, filepath.Base(path))
		case info.Size() > maxSendSize:
			closeAll()
			return nil, nil, fmt.Sprintf(m.TransferTooBigFmt, filepath.Base(path))
		}
		files = append(files, zmodem.SendFile{Name: filepath.Base(path), Size: info.Size(), ModTime: info.ModTime(), Mode: uint32(info.Mode().Perm()), Source: file})
	}
	return files, opened, ""
}

func (state *State) paintXYDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m, theme := state.messages.Tools, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitXYDismiss, CloseID: hitXYClose, InitialFocusID: hitXYStart,
		ContentSize: gfx.Size{Width: 540}, FitBody: true, BodyHeight: 138, // two labelled choices
	})
	layout := dialog.Begin(m.XYTitle, m.XYDescription)
	// Two labelled choices, one under the other.
	var protocol, direction ui.Box
	f := state.partLayout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(4, 4, 0, 4).Gap(8).Children(func() {
		protocol = f.Slot().Height(62).Shrink(0)
		direction = f.Slot().Height(62).Shrink(0)
	})
	f.End()
	content := components.BeginField(canvas, theme, protocol.Rect()).Paint(m.XYProtocol, "", "")
	content.Height = 32
	state.drawDialogChoice(canvas, content, &state.xy.modeChoice, "xy-dialog-mode", xyModeChoice)
	content = components.BeginField(canvas, theme, direction.Rect()).Paint(m.XYDirection, "", "")
	content.Height = 32
	state.drawDialogChoice(canvas, content, &state.xy.directionChoice, "xy-dialog-direction", xyDirectionChoice)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitXYCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitXYStart, Label: m.XYStart},
	)
	dialog.End()
}
