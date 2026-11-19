package workbench

import (
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type terminalFindOrigin string

const (
	findSessionTop    terminalFindOrigin = "session_top"
	findSessionBottom terminalFindOrigin = "session_bottom"
	findViewTop       terminalFindOrigin = "view_top"
	findViewBottom    terminalFindOrigin = "view_bottom"
)

var terminalFindOrigins = [...]terminalFindOrigin{findSessionTop, findSessionBottom, findViewTop, findViewBottom}

type terminalFindPreferences struct {
	Foreground string             `json:"foreground"`
	Background string             `json:"background"`
	Opacity    int                `json:"opacity"`
	Origin     terminalFindOrigin `json:"origin"`
}

var defaultTerminalFindPreferences = terminalFindPreferences{
	Foreground: "#1A1400", Background: "#FFFF00", Opacity: 100, Origin: findViewBottom,
}

func (p terminalFindPreferences) normalized() terminalFindPreferences {
	if p == (terminalFindPreferences{}) {
		return defaultTerminalFindPreferences
	}
	if _, ok := components.ParseColorHex(p.Foreground, false); !ok {
		p.Foreground = defaultTerminalFindPreferences.Foreground
	}
	if _, ok := components.ParseColorHex(p.Background, false); !ok {
		p.Background = defaultTerminalFindPreferences.Background
	}
	p.Opacity = min(max(p.Opacity, 0), 100)
	switch p.Origin {
	case findSessionTop, findSessionBottom, findViewTop, findViewBottom:
	default:
		p.Origin = findViewBottom
	}
	return p
}

func (state *State) findOriginLabel(origin terminalFindOrigin) string {
	m := state.messages.FindSettings
	switch origin {
	case findSessionTop:
		return m.SessionTop
	case findSessionBottom:
		return m.SessionBottom
	case findViewTop:
		return m.ViewTop
	default:
		return m.ViewBottom
	}
}

var findSettingsHits = tree.Feature("terminal-find-preferences")
var (
	hitFindSettingsDismiss = tree.ID(findSettingsHits, "dismiss")
	hitFindSettingsReset   = tree.ID(findSettingsHits, "reset")
	hitFindSettingsOpacity = tree.ID(findSettingsHits, "opacity")
	hitFindSettingsOrigin  = tree.ID(findSettingsHits, "origin")
	hitFindForeground      = tree.ID(findSettingsHits, "foreground")
	hitFindBackground      = tree.ID(findSettingsHits, "background")
)

// findPreference names one row of the find preferences.
type findPreference uint8

const (
	findPreferenceForeground findPreference = iota
	findPreferenceBackground
	findPreferenceOpacity
	findPreferenceOrigin
)

type terminalFindSettingsUI struct {
	open    bool
	session uint32
	// foreground and background are the colors the page pickers are bound to:
	// copied from the preferences as the page declares them, and written back
	// when a picker applies its panel.
	foreground, background gfx.Color
	foregroundPicker       ui.ColorPickerState
	backgroundPicker       ui.ColorPickerState
	// opacity and originChoice are the page controls' values, copied from the
	// preferences as the page declares them and written back when they change.
	opacity      ui.SliderState
	originChoice terminalFindOrigin
	// focus is the control the next paint gives the keyboard back to, after the
	// settings popover closes.
	focus gfx.ID
}

func (v *terminalFindSettingsUI) close() {
	v.open = false
	v.focus = gfx.ID{}
	// A picker whose surface left the window would otherwise reopen its panel
	// the next time the surface is declared; the zero value is closed.
	v.foregroundPicker = ui.ColorPickerState{}
	v.backgroundPicker = ui.ColorPickerState{}
}

func (state *State) findSettingsOnPage() bool {
	return state.modal == modalNone && state.activeSection == sectionSettings &&
		state.settingsCategory == settingsTerminal && !state.settingsSearching()
}

func (state *State) findSettingsOnBar() bool {
	v := &state.findSettingsUI
	session, ok := state.activeTerminalFind()
	return v.open && ok && session.ID == v.session && session.term.find.open
}

// applyFindPreferences stores the preferences and repaints an open find bar.
// A drag calls this on every move and saves once when the pointer is released.
func (state *State) applyFindPreferences(p terminalFindPreferences) {
	if p == state.terminalFindPrefs {
		return
	}
	originChanged := p.Origin != state.terminalFindPrefs.Origin
	state.terminalFindPrefs = p
	if session, ok := state.activeTerminalFind(); ok && session.term.find.open && originChanged {
		find := &session.term.find
		find.start = p.Origin
		find.active = -1
		find.refresh(session.term.screen, state.messages, true)
	}
}

func (state *State) setFindPreferences(p terminalFindPreferences) {
	state.applyFindPreferences(p)
	state.save()
}

func (state *State) findHighlightColors(preview bool) (ink, background gfx.Color) {
	p := state.terminalFindPrefs
	ink, _ = components.ParseColorHex(p.Foreground, false)
	background, _ = components.ParseColorHex(p.Background, false)
	if preview {
		v := &state.findSettingsUI
		if color, ok := v.foregroundPicker.Draft(); ok {
			ink = color
		}
		if color, ok := v.backgroundPicker.Draft(); ok {
			background = color
		}
	}
	background.Alpha = float32(p.Opacity) / 100
	return ink, background
}

func (state *State) paintFindPreview(canvas *gfx.Canvas, bounds gfx.Rect) {
	ink, background := state.findHighlightColors(true)
	canvas.Rounded(bounds, radius.control, state.termPal.terminal)
	canvas.Rounded(bounds, radius.control, background)
	canvas.LabelCenter(state.messages.FindSettings.Preview, bounds, gfx.TextStyle{Size: metrics.FontSizeM, Color: ink})
}

// declareFindColor declares one of the two color preferences: the hex it holds,
// and the page picker over it. The trigger keeps the 34px slot the hand-painted
// one had, and the panel keeps the preferences panel's words.
func (state *State) declareFindColor(c ui.C, foreground bool) ui.Box {
	v, m := &state.findSettingsUI, state.messages
	label, value := m.FindSettings.Foreground, state.terminalFindPrefs.Foreground
	picker, target := &v.foregroundPicker, &v.foreground
	pickerID := hitFindForeground
	if !foreground {
		label, value = m.FindSettings.Background, state.terminalFindPrefs.Background
		picker, target, pickerID = &v.backgroundPicker, &v.background, hitFindBackground
	}
	// The picker reports what it applied once, after the paint that applied it.
	// A cleared color is no color, which the preference keeps as its default.
	if picker.Applied() {
		*target = state.applyFindColor(foreground, *target)
		state.save()
	}
	if !picker.Open() {
		if color, ok := components.ParseColorHex(value, false); ok {
			*target = color
		} else {
			*target = gfx.Color{}
		}
	}
	// The row's hex follows the staged color while the panel is open, as the
	// hand-painted trigger's text did.
	shown := *target
	if color, ok := picker.Draft(); ok {
		shown = color
	}
	var buf [9]byte
	hex := string(components.ColorPickerValue{}.WithColor(shown).AppendHex(buf[:0], false))
	row := ui.Row(c).Grow(1).Gap(8).Items(ui.Center)
	row.Children(func() {
		ui.Text(c, hex).FontSize(metrics.FontSizeS).Mono().Color(state.pal.muted).Grow(1).Ellipsis()
		ui.ColorPickerID(c, pickerID, picker, target, label).
			PanelLabels(components.ColorPickerLabels{
				Color: label, Saturation: m.ColorPickerSV, Hue: m.ColorPickerHue,
				Hex: m.FieldColorCustom, Clear: m.FindSettings.Reset, Cancel: m.Cancel, Apply: m.ColorPickerApply,
			}).Size(34, 34).Shrink(0)
	})
	return row
}

// applyFindColor stores the color a picker applied and returns the color the
// picker should show next: Clear leaves it transparent, which the preference
// keeps as its default color.
func (state *State) applyFindColor(foreground bool, color gfx.Color) gfx.Color {
	p := state.terminalFindPrefs
	value := string(components.ColorPickerValue{}.WithColor(color).AppendHex(nil, false))
	if color.Alpha == 0 {
		if foreground {
			value = defaultTerminalFindPreferences.Foreground
		} else {
			value = defaultTerminalFindPreferences.Background
		}
		color, _ = components.ParseColorHex(value, false)
	}
	if foreground {
		p.Foreground = value
	} else {
		p.Background = value
	}
	state.applyFindPreferences(p)
	return color
}

// declareFindPreference declares one of the find preference controls in a row of
// the page: the two colors are page pickers, and the opacity and the origin are
// page controls.
func (state *State) declareFindPreference(c ui.C, which findPreference) ui.Box {
	v, m := &state.findSettingsUI, state.messages.FindSettings
	switch which {
	case findPreferenceForeground:
		return state.declareFindColor(c, true)
	case findPreferenceBackground:
		return state.declareFindColor(c, false)
	case findPreferenceOpacity:
		if value := float32(state.terminalFindPrefs.Opacity); v.opacity.Value != value {
			v.opacity.Value = value
		}
		percent := strconv.Itoa(state.terminalFindPrefs.Opacity) + "%"
		row := ui.Row(c).Grow(1).Gap(8).Items(ui.Center)
		row.Children(func() {
			slider := ui.Slider(c, &v.opacity).Range(0, 100).Step(1).Label(m.Opacity).Hit(hitFindSettingsOpacity, m.Opacity).Grow(1)
			if slider.Changed() {
				opacity := int(v.opacity.Value)
				c.After(func() {
					p := state.terminalFindPrefs
					p.Opacity = opacity
					state.setFindPreferences(p)
				})
			}
			ui.Text(c, percent).FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(36).Shrink(0).TextEnd()
		})
		return row
	case findPreferenceOrigin:
		if v.originChoice != state.terminalFindPrefs.Origin {
			v.originChoice = state.terminalFindPrefs.Origin
		}
		origins := terminalFindOrigins[:]
		selector := ui.Select(c, &v.originChoice, origins, state.findOriginLabel).Label(m.Origin).Hit(hitFindSettingsOrigin, m.Origin).Grow(1)
		if selector.Changed() {
			origin := v.originChoice
			c.After(func() {
				p := state.terminalFindPrefs
				p.Origin = origin
				state.setFindPreferences(p)
			})
		}
		return selector
	}
	return state.declareFindPreview(c)
}

// declareFindPreview declares the strip that shows the highlight colors, the
// opacity included, as they would read in the terminal.
func (state *State) declareFindPreview(c ui.C) ui.Box {
	preview := ui.Slot(c).Grow(1).Height(34)
	preview.Draw(func(d *ui.DrawContext) { state.paintFindPreview(d.Canvas, d.Rect) })
	return preview
}

// settleFindPreferences gives the keyboard back to where the settings were
// closed from, and leaves the pickers when neither surface shows them.
func (state *State) settleFindPreferences(canvas *gfx.Canvas) {
	v := &state.findSettingsUI
	if !v.focus.IsZero() {
		canvas.RequestFocus(v.focus)
		v.focus = gfx.ID{}
	}
	if !state.findSettingsOnBar() && !state.findSettingsOnPage() {
		v.close()
	}
}

// declareFindSettingsPopover declares the preferences popover of the find bar in
// the page, hanging off the settings button. The settings page's copy of these
// controls is declared in its own rows.
func (state *State) declareFindSettingsPopover(c ui.C, anchor ui.Box) {
	v, m := &state.findSettingsUI, state.messages.FindSettings
	if !v.open || !state.findSettingsOnBar() {
		return
	}
	dismissed := ui.PopoverWith(c, anchor, ui.PopoverSpec{
		Scope: hitFindSettingsDismiss, DismissID: hitFindSettingsDismiss,
		Align: ui.End, Offset: 6, Width: 370, Title: m.Title,
		ConsumeOutsidePress: true,
	}, &v.open, func() {
		for _, setting := range [...]struct {
			which findPreference
			label string
		}{
			{findPreferenceForeground, m.Foreground}, {findPreferenceBackground, m.Background},
			{findPreferenceOpacity, m.Opacity}, {findPreferenceOrigin, m.Origin},
		} {
			ui.Row(c).Height(32).Gap(8).Items(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, setting.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.text).Width(144).Shrink(0).Ellipsis()
				state.declareFindPreference(c, setting.which)
			})
		}
		state.declareFindPreview(c).FillWidth().Height(32)
		ui.Button(c, m.Reset).Outline().Hit(hitFindSettingsReset, m.Reset).FillWidth().Shrink(0)
	})
	if dismissed {
		// The page closed the panel; the keyboard goes back to the find field,
		// as it does when the application's own dismiss handler closes it.
		c.After(func() {
			v.focus = hitTerminalFindField
			if session, ok := state.activeTerminalFind(); ok {
				v.focus = state.paneIdentity(session, hitTerminalFindField)
			}
		})
	}
}

func (state *State) clickFindPreferences(id gfx.ID) bool {
	v := &state.findSettingsUI
	if !state.findSettingsOnBar() && !state.findSettingsOnPage() {
		return false
	}
	switch id {
	case hitFindSettingsDismiss:
		v.close()
		v.focus = hitTerminalFindField
		if session, ok := state.activeTerminalFind(); ok {
			v.focus = state.paneIdentity(session, hitTerminalFindField)
		}
	case hitFindSettingsReset:
		state.setFindPreferences(defaultTerminalFindPreferences)
	default:
		return false
	}
	return true
}

func (state *State) paintTerminalFindSettingsGroup(page *settingsPage) {
	m := state.messages.FindSettings
	page.group(m.Title, func() {
		c := page.c
		for _, setting := range [...]struct {
			which findPreference
			label string
			width float32
			id    gfx.ID
		}{
			{findPreferenceForeground, m.Foreground, 104, hitFindForeground},
			{findPreferenceBackground, m.Background, 104, hitFindBackground},
			{findPreferenceOpacity, m.Opacity, 182, hitFindSettingsOpacity},
			{findPreferenceOrigin, m.Origin, 192, hitFindSettingsOrigin},
		} {
			page.lineFor(setting.id, setting.label, "", setting.width, func() {
				state.declareFindPreference(c, setting.which)
			})
		}
		page.lineWith(m.Preview, "", 192, func() { state.declareFindPreview(c) })
		page.actionRow(m.Title, "", false, components.ButtonAction{ID: hitFindSettingsReset, Label: m.Reset, Variant: components.ButtonOutline})
	})
}
