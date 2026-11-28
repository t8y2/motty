package workbench

import "github.com/vivaui/viva/ui"

// An RDP host's remote interface scale is the DesktopScaleFactor its server is
// asked for, in the handshake and in every resize after it. It is the remote
// session's DPI, not the scale this computer draws at: asking a shared Windows
// session for 200% because this display is a Retina one changes its taskbar
// and fonts for everyone who signs in to it (#1465). So a host asks for 100% and
// the picture is scaled on this side, unless it chose to follow this display.

// desktopScale is a remote interface scale choice. The zero value is 100%.
type desktopScale uint8

const (
	desktopScaleStandard desktopScale = iota
	// desktopScaleDisplay asks for the pane's device pixels at the display's
	// scale, so the picture is drawn pixel for pixel.
	desktopScaleDisplay
)

// desktopScaleChoices are what the remote scale select lists.
var desktopScaleChoices = [...]desktopScale{desktopScaleStandard, desktopScaleDisplay}

// saved is the choice as the catalog writes it: nothing for 100%, so a record
// that never chose stays what it was.
func (scale desktopScale) saved() string {
	if scale == desktopScaleDisplay {
		return "display"
	}
	return ""
}

// parseDesktopScale reads a saved choice; anything it does not know is 100%.
func parseDesktopScale(saved string) desktopScale {
	if saved == "display" {
		return desktopScaleDisplay
	}
	return desktopScaleStandard
}

// remoteScale is the interface scale a host's server is asked for on a display
// of pixelScale: 100% unless the host follows the display, and always 100% at a
// fixed resolution, which is a size, not a scale.
func (host Host) remoteScale(pixelScale float32) float32 {
	if _, fixed := host.fixedDesktopSize(); fixed || host.DesktopScale != desktopScaleDisplay || pixelScale <= 0 {
		return 1
	}
	return pixelScale
}

func (state *State) desktopScaleLabel(scale desktopScale) string {
	if scale == desktopScaleDisplay {
		return state.messages.Desktop.RemoteScaleDisplay
	}
	return state.messages.Desktop.RemoteScaleStandard
}

// hostScaleBody is where the remote scale select of the host form was laid out.
type hostScaleBody struct {
	field  ui.FormFieldState
	box    ui.Box
	choice desktopScale
}

// declareScaleSelect lays out the remote scale select of an RDP host, under the
// resolution. A fixed resolution is always at 100%, so the select shows that and
// cannot be changed while one is chosen.
func (state *State) declareScaleSelect(c ui.C, rowHeight float32) {
	editor, form, m := &state.editor, &state.editor.form, state.messages.Desktop
	b := &form.scaleBody
	fixed := editor.resolution != (desktopSize{})
	b.choice = editor.desktopScale
	if fixed {
		b.choice = desktopScaleStandard
	}
	ui.FormField(c, &b.field, m.RemoteScaleField).FillWidth().Height(rowHeight).Children(func() {
		b.box = ui.Select(c, &b.choice, desktopScaleChoices[:], state.desktopScaleLabel).Label(m.RemoteScaleField).Disabled(fixed)
		if b.box.Changed() && !fixed {
			editor.desktopScale = b.choice
			state.dialogError = ""
		}
	})
}
