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

// The dialog's two segmented choices, declared in the dialog's body page.
var (
	xyModeChoices      = [...]xymodem.Mode{xymodem.XMODEM, xymodem.XMODEM1K, xymodem.YMODEM}
	xyDirectionChoices = [...]bool{true, false}
)

// xySetup is the dialog's choices.
type xySetup struct {
	mode    xymodem.Mode
	sending bool
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

// xyDialogBodyHeight is the dialog's body: two labelled choices, one under the
// other.
const xyDialogBodyHeight = 138

// declareXYDialog declares the setup dialog in the workbench page, whose
// choices take the window's keys and pointer as any other page control does.
func (state *State) declareXYDialog(c ui.C) {
	m := state.messages.Tools
	viewport := dialogViewport(c)
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: 540}, FitBody: true,
	}).Layout(m.XYDescription)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.XYTitle, Description: m.XYDescription, Width: 540,
		Height: layout.Content.Height + xyDialogBodyHeight,
		Layer:  dialogLayer, Scope: dialogScope,
		DismissID: hitXYDismiss, CloseID: hitXYClose, InitialFocusID: hitXYStart,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().PadSides(4, 4, 0, 4).Gap(8).Children(func() {
			hostFormChoice(c, m.XYProtocol, 62, &state.xy.mode, xyModeChoices[:], state.xyModeChoiceLabel)
			hostFormChoice(c, m.XYDirection, 62, &state.xy.sending, xyDirectionChoices[:], state.xyDirectionChoiceLabel)
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitXYCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitXYStart, Label: m.XYStart},
		)
	})
}

// openXYSetup opens the dialog for the session.
func (state *State) openXYSetup(session *Session) {
	if session.term == nil || session.term.shell == nil || session.transferBusy() {
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
	if session == nil || session.term == nil || session.term.shell == nil || session.transferBusy() {
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
	session.transfer = fileTransfer{phase: transferOffered, sessionID: session.ID, sending: setup.sending, target: target, sources: sources}
	state.startTransfer(session, engine)
	state.showNextPrompt()
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
