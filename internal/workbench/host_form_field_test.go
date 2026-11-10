package workbench

import (
	"testing"

	"github.com/vivaui/viva/field"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The proxy and jump summary rows are form fields of the host form: each keeps
// the fixed 70px row the slots had and places its control at the control slot
// the shared field layout gives a components.BeginField in the same bounds.
func TestHostFormProxyAndJumpRowsKeepTheFormFieldLayout(t *testing.T) {
	for _, protocol := range []hostProtocol{protocolSSH, protocolTelnet, protocolVNC, protocolRDP} {
		t.Run(protocolName(protocol), func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			state.openEditHostID(hostByLabel(t, state, "Production").ID)
			state.editor.setProtocol(protocol)
			canvas, _ := paintAt(state, gfx.Size{Width: 900, Height: 900})
			theme := state.Theme()
			body := &state.editor.form.body
			type rowBoxes struct {
				name           string
				label          string
				field, control ui.Box
			}
			rows := []rowBoxes{
				{"proxy", state.messages.Tools.HostProxyRow, body.proxy, body.proxyRow},
			}
			if protocol == protocolSSH {
				rows = append(rows, rowBoxes{"jump", state.messages.Tools.HostJumpRow, body.jump, body.jumpRow})
			}
			for _, row := range rows {
				bounds := row.field.Rect()
				if bounds.Width <= 0 || bounds.Height != hostFormRowHeight {
					t.Fatalf("%s field is %+v, want a %vpx row", row.name, bounds, hostFormRowHeight)
				}
				layout := field.Measure(theme, bounds, field.Spec{Title: row.label})
				if got := row.control.Rect(); got != layout.Control {
					t.Fatalf("%s control is %+v, the shared layout says %+v", row.name, got, layout.Control)
				}
				if layout.Control.Y < bounds.Y || layout.Control.Y+layout.Control.Height > bounds.Y+bounds.Height {
					t.Fatalf("%s control %+v overflows the field %+v", row.name, layout.Control, bounds)
				}
			}
			for _, id := range []gfx.ID{hitHostProxyConfigure, hitHostJumpConfigure} {
				hit, ok := hitByID(canvas, id)
				if protocol != protocolSSH && id == hitHostJumpConfigure {
					continue
				}
				if !ok {
					t.Fatalf("the row's button %v was not painted", id)
				}
				if hit.Bounds.Width <= 0 || hit.Bounds.Height <= 0 {
					t.Fatalf("the row's button %v has no bounds: %+v", id, hit.Bounds)
				}
			}
		})
	}
}

// The declared fields keep the rows' buttons in the form's tab ring: the proxy
// button always, the jump button only while its route can be configured.
func TestHostFormProxyAndJumpButtonsStayInTheTabRing(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openEditHostID(hostByLabel(t, state, "Production").ID)
	paintAt(state, gfx.Size{Width: 900, Height: 900})
	proxy, jump := hostFormStopIndex(state, hitHostProxyConfigure), hostFormStopIndex(state, hitHostJumpConfigure)
	if proxy < 0 || jump < 0 {
		t.Fatalf("tab stops: proxy=%d jump=%d", proxy, jump)
	}
	if proxy >= jump {
		t.Fatalf("the proxy row's stop %d is not before the jump row's %d", proxy, jump)
	}

	// An unsaved host's jump route cannot be configured, so its button is not a
	// tab stop and the fields after it are not skipped.
	state.openHostDialog()
	paintAt(state, gfx.Size{Width: 900, Height: 900})
	if hostFormStopIndex(state, hitHostProxyConfigure) < 0 {
		t.Fatal("the proxy row's button left the tab ring")
	}
	if index := hostFormStopIndex(state, hitHostJumpConfigure); index >= 0 {
		t.Fatalf("the disabled jump button is a tab stop at %d", index)
	}
}

func hostFormStopIndex(state *State, id gfx.ID) int {
	for i := 0; i < state.editor.form.count; i++ {
		if state.editor.form.stops[i].id == id {
			return i
		}
	}
	return -1
}
