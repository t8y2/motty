package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// remoteProcessWheel scrolls the open detail dialog's text, else the page List
// under the pointer. The List owns its scroll offset and the wheel's steps.
func (state *State) remoteProcessWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	view := &state.remote
	if !state.remoteToolsVisible() {
		return gfx.EventResult{}, false
	}
	if view.containerDetails.open {
		if result, handled := state.remoteContainerDetailsWheel(event); handled {
			return result, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	if view.processDetails.open {
		if result, handled := state.remoteProcessEnvironmentWheel(event); handled {
			return result, true
		}
		return gfx.EventResult{Handled: true}, true
	}
	if view.auto.open {
		return gfx.EventResult{}, false
	}
	result := state.windowPage().RouteWheel(event)
	return result, result.Handled || result.Repaint
}

// processTableSchema is the process table's columns and headings for a width.
// The five leading columns narrow together in a window too small for them; the
// last takes what is left.
func (state *State) processTableSchema(width float32) ([]components.TableColumn, []string) {
	m := state.messages.Tools
	view := &state.remote
	view.processColumns = [remoteMaxColumns]components.TableColumn{
		{Width: 72, Alignment: components.TableAlignRight},
		{Width: 110},
		{Width: 78, Alignment: components.TableAlignRight},
		{Width: 88, Alignment: components.TableAlignRight},
		{Width: 108},
		{Width: 0},
	}
	view.columns = [remoteMaxColumns]string{m.RemoteColPID, m.RemoteColUser, m.RemoteColCPU, m.RemoteColMemory, m.RemoteColTime, m.RemoteColCommand}
	const fixed = 72 + 110 + 78 + 88 + 108
	if width < fixed+140 {
		available := max(width-120, 1)
		scale := min(available/fixed, 1)
		for index := range view.processColumns[:5] {
			view.processColumns[index].Width *= scale
		}
	}
	return view.processColumns[:6], view.columns[:6]
}
