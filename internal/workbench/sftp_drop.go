package workbench

import (
	"fmt"
	"math"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/vivaui/viva/ui"
)

// paintSFTPDropOverlay is presentation only: it declares no hits, so the file
// row under the pointer remains the target of the eventual system drop.
func (state *State) paintSFTPDropOverlay(canvas *ui.Canvas, bounds ui.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	canvas.Clip(bounds)
	defer canvas.Unclip()

	gray := ui.RGB(128, 128, 128)
	// Quiet the list below the hint without turning the drop area into a solid
	// slab. Both surfaces and the icon stay neutral in either theme.
	canvas.Fill(bounds, withAlpha(state.pal.workspace, 0.92))
	canvas.Fill(bounds, withAlpha(gray, 0.08))
	const stroke, dash, gap = float32(1.5), float32(8), float32(6)
	// End both edges with a complete dash so the four square corners meet.
	dashes := func(length float32, paint func(float32, float32)) {
		count := max(int(math.Round(float64((length+gap)/(dash+gap)))), 1)
		part := min(dash, length)
		step := float32(0)
		if count > 1 {
			step = (length - part) / float32(count-1)
		}
		for index := 0; index < count; index++ {
			paint(float32(index)*step, part)
		}
	}
	dashes(bounds.Width, func(offset, length float32) {
		canvas.Fill(ui.Rect{X: bounds.X + offset, Y: bounds.Y, Width: length, Height: stroke}, gray)
		canvas.Fill(ui.Rect{X: bounds.X + offset, Y: bounds.Y + bounds.Height - stroke, Width: length, Height: stroke}, gray)
	})
	dashes(bounds.Height, func(offset, length float32) {
		canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + offset, Width: stroke, Height: length}, gray)
		canvas.Fill(ui.Rect{X: bounds.X + bounds.Width - stroke, Y: bounds.Y + offset, Width: stroke, Height: length}, gray)
	})

	label := state.messages.Tools.DropUploadHint
	textStyle := ui.TextStyle{Size: 15, Weight: ui.WeightMedium, Color: state.pal.text}
	icon := ui.Rect{Width: 36, Height: 36}
	text := ui.Rect{X: bounds.X + 12, Width: max(bounds.Width-24, 0), Height: 22}
	if bounds.Height < 100 {
		// A folder row has room for a single centered icon-and-label line.
		textStyle.Size = 13
		icon.Width, icon.Height = 16, 16
		width := min(canvas.TextWidthWeight(label, textStyle.Size, textStyle.Weight), max(bounds.Width-44, 0))
		icon.X = bounds.X + (bounds.Width-icon.Width-8-width)/2
		icon.Y = bounds.Y + (bounds.Height-icon.Height)/2
		text = ui.Rect{X: icon.X + icon.Width + 8, Y: bounds.Y + (bounds.Height-20)/2, Width: width, Height: 20}
	} else {
		const groupHeight = float32(72)
		icon.X = bounds.X + (bounds.Width-icon.Width)/2
		icon.Y = bounds.Y + (bounds.Height-groupHeight)/2
		text.Y = icon.Y + icon.Height + 14
	}
	paintIcon(canvas, iconUpload, icon, state.pal.muted)
	canvas.LabelCenter(label, text, textStyle)
}

// Files dragged in from the system. Over the file panel they are uploaded: onto a
// folder row into that folder, anywhere else on the panel into the folder on
// show. Over the terminal of a shell on this computer they are typed as paths,
// quoted, as other terminals do. Nowhere else takes them.

// pointInContext reports whether a window point is on the side panel.
func (state *State) pointInContext(x, y float32) bool {
	return state.contextRect.Width > 0 && pointInRect(x, y, state.contextRect)
}

// dropPanel is the file panel of the active session when it can take an upload
// at the point: it shows the server's files, is not busy with a dialog or the
// editor, and the point is on it.
func (state *State) dropPanel(x, y float32) (*Session, *sftpView) {
	if state.activeSection != sectionTerminal || state.modal != modalNone || !state.pointInContext(x, y) {
		return nil, nil
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || session.conn == nil {
		return nil, nil
	}
	view := session.files
	if view.local || view.dialog != sftpDialogNone || view.editPath != "" || view.path == "" {
		return nil, nil
	}
	return session, view
}

// dropFolderRow is the row of a folder under the pointer, or -1.
func (state *State) dropFolderRow(view *sftpView, target ui.ID) int {
	local, ok := tree.Local(contextMount, target)
	if !ok || local.Scope() != hitSFTPEntryList {
		return -1
	}
	index := view.first + item(local)
	if index < 0 || index >= len(view.rows) || !view.rows[index].entry.IsDir() {
		return -1
	}
	return index
}

// FileDrop receives files dragged from the system over the window.
func (state *State) FileDrop(event ui.FileDropEvent) ui.EventResult {
	switch event.Phase {
	case ui.FileDropLeave:
		return ui.EventResult{Repaint: state.clearDrop()}
	case ui.FileDropOver:
		if _, view := state.dropPanel(event.X, event.Y); view != nil {
			row := state.dropFolderRow(view, event.Target)
			changed := !view.dropping || view.dropRow != row
			view.dropping, view.dropRow = true, row
			return ui.EventResult{Handled: true, Repaint: changed || state.clearDropExcept(view)}
		}
		repaint := state.clearDrop()
		return ui.EventResult{Handled: state.terminalTakesPaths(event.X, event.Y), Repaint: repaint}
	}
	repaint := state.clearDrop()
	if session, view := state.dropPanel(event.X, event.Y); view != nil {
		remoteDir := view.path
		if row := state.dropFolderRow(view, event.Target); row >= 0 {
			remoteDir = path.Join(view.path, view.rows[row].entry.Name)
		}
		queued := 0
		for _, local := range event.Paths {
			info, err := os.Stat(local)
			if err != nil {
				continue
			}
			state.queueSFTPUploadTo(view, local, info.IsDir(), remoteDir)
			queued++
		}
		if queued > 0 {
			view.status = fmt.Sprintf(state.messages.Tools.DropUploadingFmt, queued, remoteDir)
		}
		_ = session
		return ui.EventResult{Handled: queued > 0, Repaint: true}
	}
	if state.terminalTakesPaths(event.X, event.Y) {
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		session.term.send([]byte(quotedPaths(event.Paths) + " "))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{Repaint: repaint}
}

// terminalTakesPaths reports whether the point is on the terminal of a live
// shell on this computer, which can be handed paths to type.
func (state *State) terminalTakesPaths(x, y float32) bool {
	if state.activeSection != sectionTerminal || state.modal != modalNone || state.pointInContext(x, y) {
		return false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	return ok && session.isLocal() && session.term != nil && session.term.shell != nil
}

// clearDrop ends a hover over the panel and reports whether anything showed it.
func (state *State) clearDrop() bool {
	changed := false
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil && view.dropping {
			view.dropping, view.dropRow = false, -1
			changed = true
		}
	}
	return changed
}

// clearDropExcept clears the hover of every panel but view's.
func (state *State) clearDropExcept(keep *sftpView) bool {
	changed := false
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil && view != keep && view.dropping {
			view.dropping, view.dropRow = false, -1
			changed = true
		}
	}
	return changed
}

// quotedPaths makes paths safe to type into the shell of this computer.
func quotedPaths(paths []string) string {
	quoted := make([]string, len(paths))
	for index, p := range paths {
		if runtime.GOOS == "windows" {
			quoted[index] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		} else {
			quoted[index] = shellQuote(p)
		}
	}
	return strings.Join(quoted, " ")
}
