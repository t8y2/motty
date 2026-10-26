package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
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
	hitXYProtocolX  = tree.ID(transferXyHits, "xy-protocol-x")
	hitXYProtocol1K = tree.ID(transferXyHits, "xy-protocol1-k")
	hitXYProtocolY  = tree.ID(transferXyHits, "xy-protocol-y")
	hitXYSend       = tree.ID(transferXyHits, "xy-send")
	hitXYReceive    = tree.ID(transferXyHits, "xy-receive")
	hitXYStart      = tree.ID(transferXyHits, "xy-start")
	hitXYCancel     = tree.ID(transferXyHits, "xy-cancel")
	hitXYClose      = tree.ID(transferXyHits, "xy-close")
	hitXYDismiss    = tree.ID(transferXyHits, "xy-dismiss")
)

// xySetup is the dialog's choices.
type xySetup struct {
	mode    xymodem.Mode
	sending bool
}

// openXYSetup opens the dialog for the session.
func (state *State) openXYSetup(session *Session) {
	if session.term == nil || session.term.shell == nil || state.transfer.phase != transferNone {
		return
	}
	state.xy = xySetup{mode: xymodem.YMODEM, sending: true}
	state.modal, state.modalSession = modalXYTransfer, session.ID
}

func (state *State) clickXYSetup(id ui.ID) {
	switch id {
	case hitXYProtocolX:
		state.xy.mode = xymodem.XMODEM
	case hitXYProtocol1K:
		state.xy.mode = xymodem.XMODEM1K
	case hitXYProtocolY:
		state.xy.mode = xymodem.YMODEM
	case hitXYSend:
		state.xy.sending = true
	case hitXYReceive:
		state.xy.sending = false
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

func (state *State) paintXYDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages.Tools, state.Theme()
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitXYDismiss, CloseID: hitXYClose, InitialFocusID: hitXYStart,
		ContentSize: ui.Size{Width: 540}, FitBody: true, BodyHeight: 138, // two labelled choices
	})
	layout := dialog.Begin(m.XYTitle, m.XYDescription)
	protocols := [...]components.SegmentOption{
		{ID: hitXYProtocolX, Label: "XMODEM"}, {ID: hitXYProtocol1K, Label: "XMODEM-1K"}, {ID: hitXYProtocolY, Label: "YMODEM"},
	}
	selected := hitXYProtocolY
	switch state.xy.mode {
	case xymodem.XMODEM:
		selected = hitXYProtocolX
	case xymodem.XMODEM1K:
		selected = hitXYProtocol1K
	}
	directions := [...]components.SegmentOption{{ID: hitXYSend, Label: m.XYSend}, {ID: hitXYReceive, Label: m.XYReceive}}
	selectedDirection := hitXYReceive
	if state.xy.sending {
		selectedDirection = hitXYSend
	}
	// Two labelled choices, one under the other.
	var protocol, direction flex.Box
	f := state.partLayout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(4, 4, 0, 4).Gap(8).Children(func() {
		protocol = f.Slot().Height(62).Shrink(0)
		direction = f.Slot().Height(62).Shrink(0)
	})
	f.End()
	content := components.BeginField(canvas, theme, protocol.Rect()).Paint(m.XYProtocol, "", "")
	content.Height = 32
	components.BeginSegmentedControl(canvas, theme, content, protocols[:], selected).Paint()
	content = components.BeginField(canvas, theme, direction.Rect()).Paint(m.XYDirection, "", "")
	content.Height = 32
	components.BeginSegmentedControl(canvas, theme, content, directions[:], selectedDirection).Paint()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitXYCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitXYStart, Label: m.XYStart},
	)
	dialog.End()
}
