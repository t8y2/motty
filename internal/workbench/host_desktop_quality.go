package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"motty/internal/desktop"
)

// A remote desktop host chooses how much bandwidth its picture may use: the
// color depth of the pixels (VNC 32, 16 or 8 bits; RDP 32, 24 or 16), the
// pixel encoding a VNC server is asked to prefer, and the visual effects an RDP
// server may render. The first choice of each is what a host has always had, so
// a host that never opened this section behaves as before and is saved without
// it.

// defaultColorDepth is the color depth of a host that never chose one.
const defaultColorDepth = 32

var (
	vncColorDepths = [...]int{32, 16, 8}
	rdpColorDepths = [...]int{32, 24, 16}
	vncEncodings   = [...]desktop.Encoding{desktop.EncodingAuto, desktop.EncodingZRLE, desktop.EncodingHextile, desktop.EncodingRaw}
)

// vncColorDepth is the saved color depth of a VNC host: zero for the default,
// and zero too for a depth VNC has no format for, such as one left by an RDP
// host of the same record.
func vncColorDepth(depth int) int { return savedColorDepth(depth, vncColorDepths[1:]) }

// rdpColorDepth is vncColorDepth for RDP.
func rdpColorDepth(depth int) int { return savedColorDepth(depth, rdpColorDepths[1:]) }

func savedColorDepth(depth int, others []int) int {
	for _, other := range others {
		if depth == other {
			return depth
		}
	}
	return 0
}

// colorDepthOrDefault is the depth a select shows for a stored value.
func colorDepthOrDefault(depth int) int {
	if depth == 0 {
		return defaultColorDepth
	}
	return depth
}

func (state *State) colorDepthLabel(depth int) string {
	m := state.messages.Desktop
	if depth == defaultColorDepth {
		return fmt.Sprintf(m.ColorDepthDefaultFmt, depth)
	}
	return fmt.Sprintf(m.ColorDepthFmt, depth)
}

func (state *State) desktopEncodingLabel(encoding desktop.Encoding) string {
	m := state.messages.Desktop
	switch encoding {
	case desktop.EncodingZRLE:
		return m.EncodingZRLE
	case desktop.EncodingHextile:
		return m.EncodingHextile
	case desktop.EncodingRaw:
		return m.EncodingRaw
	}
	return m.EncodingAuto
}

// desktopQualityRowsHeight is the room of the quality section for a protocol: a
// heading, the color depth, and the VNC encoding or the RDP effects.
func desktopQualityRowsHeight(protocol hostProtocol) float32 {
	if protocol == protocolRDP {
		return groupHeadingHeight + hostFormRowHeight + rdpEffectCount*(12+dialogCheckHeight) + 16
	}
	return groupHeadingHeight + 2*hostFormRowHeight + 16
}

// rdpEffectCount is how many effect switches the RDP section has.
const rdpEffectCount = 7

// hostQualityBody is where the quality section's selects were laid out.
type hostQualityBody struct {
	depthField, encodingField ui.FormFieldState
	depthBox, encodingBox     ui.Box
	depthChoice               int
	encodingChoice            desktop.Encoding
}

// effectSwitches are the RDP effect switches in the order they are listed:
// each one's hit, label and the setting it flips.
func (state *State) effectSwitches() [rdpEffectCount]struct {
	id      gfx.ID
	label   string
	setting *bool
} {
	m, effects := state.messages.Desktop, &state.editor.effects
	return [rdpEffectCount]struct {
		id      gfx.ID
		label   string
		setting *bool
	}{
		{hitDialogEffectWallpaper, m.EffectWallpaper, &effects.Wallpaper},
		{hitDialogEffectFontSmoothing, m.EffectFontSmoothing, &effects.FontSmoothing},
		{hitDialogEffectComposition, m.EffectComposition, &effects.Composition},
		{hitDialogEffectWindowDrag, m.EffectWindowDrag, &effects.WindowDrag},
		{hitDialogEffectAnimations, m.EffectAnimations, &effects.Animations},
		{hitDialogEffectThemes, m.EffectThemes, &effects.Themes},
		{hitDialogEffectCursorShadow, m.EffectCursorShadow, &effects.CursorShadow},
	}
}

// declareDesktopQualityRows lays out the quality section of a VNC or RDP host.
func (state *State) declareDesktopQualityRows(c ui.C) {
	editor, form, m := &state.editor, &state.editor.form, state.messages.Desktop
	b := &form.qualityBody
	*b = hostQualityBody{}
	rowHeight := float32(hostFormRowHeight)
	rdp := editor.form.layoutProtocol == protocolRDP
	ui.Column(c).FillWidth().Children(func() {
		ui.Column(c).Height(groupHeadingHeight).Children(func() {
			ui.Column(c).Height(1).MarginSides(12, 0, 0, 0).Background(withAlpha(state.pal.line, 0.7))
			ui.Text(c, m.QualityHeading).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).MarginSides(8, 0, 0, 2)
		})
		depths := vncColorDepths[:]
		if rdp {
			depths = rdpColorDepths[:]
		}
		b.depthChoice = colorDepthOrDefault(editor.colorDepth)
		ui.FormField(c, &b.depthField, m.ColorDepthField).FillWidth().Height(rowHeight).Children(func() {
			b.depthBox = ui.Select(c, &b.depthChoice, depths, state.colorDepthLabel).Label(m.ColorDepthField)
			if b.depthBox.Changed() {
				editor.colorDepth = savedColorDepth(b.depthChoice, depths[1:])
				state.dialogError = ""
			}
		})
		if rdp {
			for _, effect := range state.effectSwitches() {
				state.declareDialogCheck(c, effect.id, effect.label, *effect.setting, 12, 0)
			}
			ui.Slot(c).Height(16)
			return
		}
		b.encodingChoice = editor.desktopEncoding
		ui.FormField(c, &b.encodingField, m.EncodingPreferenceField).FillWidth().Height(rowHeight).Children(func() {
			b.encodingBox = ui.Select(c, &b.encodingChoice, vncEncodings[:], state.desktopEncodingLabel).Label(m.EncodingPreferenceField)
			if b.encodingBox.Changed() {
				editor.desktopEncoding = b.encodingChoice
				state.dialogError = ""
			}
		})
		ui.Slot(c).Height(16)
	})
}

// clickDesktopQuality handles the effect switches of an RDP host.
func (state *State) clickDesktopQuality(id gfx.ID) bool {
	for _, effect := range state.effectSwitches() {
		if effect.id == id {
			*effect.setting = !*effect.setting
			return true
		}
	}
	return false
}
