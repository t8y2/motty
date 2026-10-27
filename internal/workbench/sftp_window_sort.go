package workbench

import (
	"cmp"
	"slices"
	"strings"
)

type sftpWindowSort struct {
	column     int
	descending bool
}

func (w *sftpWindow) sortFolder(local bool) {
	defer w.selectionChanged(local)
	folder, order := w.folder(local), w.ui.sort[sideIndex(local)]
	selected, anchor := "", ""
	if folder.selected >= 0 && folder.selected < len(folder.rows) {
		selected = folder.rows[folder.selected].entry.Name
	}
	if folder.anchor >= 0 && folder.anchor < len(folder.rows) {
		anchor = folder.rows[folder.anchor].entry.Name
	}
	for i := range folder.rows {
		if folder.rows[i].sortName == "" {
			folder.rows[i].sortName = strings.ToLower(folder.rows[i].entry.Name)
		}
	}
	slices.SortStableFunc(folder.rows, func(a, b sftpRow) int {
		if a.entry.IsDir() != b.entry.IsDir() {
			if a.entry.IsDir() {
				return -1
			}
			return 1
		}
		result := 0
		switch order.column {
		case 1:
			result = cmp.Compare(a.entry.Size, b.entry.Size)
		case 2:
			result = a.entry.ModTime.Compare(b.entry.ModTime)
		default:
			result = strings.Compare(a.sortName, b.sortName)
		}
		if result == 0 {
			result = strings.Compare(a.sortName, b.sortName)
		}
		if result == 0 {
			result = strings.Compare(a.entry.Name, b.entry.Name)
		}
		if order.descending {
			return -result
		}
		return result
	})
	for i := range folder.rows {
		if folder.rows[i].entry.Name == selected {
			folder.selected = i
		}
		if folder.rows[i].entry.Name == anchor {
			folder.anchor = i
		}
	}
}

func (view *sftpView) sortedDetached(local bool) {
	if view.detached != nil {
		view.detached.sortFolder(local)
	}
}

// Resolve by owning browser, never by whichever tab happens to be active.
func (state *State) sftpSession(view *sftpView) (*Session, bool) {
	if view.detached != nil {
		return &view.detached.session, !view.detached.closed
	}
	for i := range state.sessions {
		if state.sessions[i].files == view {
			return &state.sessions[i], true
		}
	}
	return nil, false
}
