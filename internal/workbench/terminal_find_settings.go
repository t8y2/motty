package workbench

import (
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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
	hitFindColorDismiss    = tree.ID(findSettingsHits, "color-dismiss")
	hitFindColorHex        = tree.ID(findSettingsHits, "hex")
	hitFindColorSaturation = tree.ID(findSettingsHits, "saturation")
	hitFindColorHue        = tree.ID(findSettingsHits, "hue")
	hitFindColorClear      = tree.ID(findSettingsHits, "color-clear")
	hitFindColorCancel     = tree.ID(findSettingsHits, "color-cancel")
	hitFindColorApply      = tree.ID(findSettingsHits, "color-apply")
	hitFindForeground      = tree.ID(findSettingsHits, "foreground")
	hitFindBackground      = tree.ID(findSettingsHits, "background")
)

type terminalFindSettingsUI struct {
	open    bool
	session uint32
	// anchor is where the find bar's settings button was, for the color panel;
	// foreground and background are where the color triggers were.
	anchor, foreground, background gfx.Rect
	color                          gfx.ID
	picker                         hostColorPicker
	// opacity and originChoice are the page controls' values, copied from the
	// preferences as the page declares them and written back when they change.
	opacity      ui.SliderState
	originChoice terminalFindOrigin
	focus        gfx.ID
}

func (v *terminalFindSettingsUI) close() {
	v.open = false
	v.color = gfx.ID{}
	v.focus = gfx.ID{}
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
		switch state.findSettingsUI.color {
		case hitFindForeground:
			ink = state.findSettingsUI.picker.draft.Color()
		case hitFindBackground:
			background = state.findSettingsUI.picker.draft.Color()
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

func (state *State) findColorOptions(canvas *gfx.Canvas, id gfx.ID, layer uint16) components.ColorPickerOptions {
	v, m := &state.findSettingsUI, state.messages
	p := &v.picker
	value := state.terminalFindPrefs.Foreground
	label := m.FindSettings.Foreground
	if id == hitFindBackground {
		value, label = state.terminalFindPrefs.Background, m.FindSettings.Background
	}
	color, _ := components.ParseColorHex(value, false)
	draft := components.ColorPickerValue{}.WithColor(color)
	open := v.color == id
	if open {
		draft = p.draft
	}
	focused := canvas.Focused() == hitFindColorHex
	o := components.ColorPickerOptions{
		IDs: components.ColorPickerIDs{
			Trigger: id, Dismiss: hitFindColorDismiss, Hex: hitFindColorHex, Saturation: hitFindColorSaturation,
			Hue: hitFindColorHue, Clear: hitFindColorClear, Cancel: hitFindColorCancel, Apply: hitFindColorApply,
		},
		Labels: components.ColorPickerLabels{
			Color: label, Saturation: m.ColorPickerSV, Hue: m.ColorPickerHue, Hex: m.FieldColorCustom,
			Clear: m.FindSettings.Reset, Cancel: m.Cancel, Apply: m.ColorPickerApply,
		},
		Value: draft, Open: open, SaturationText: p.summary,
		Hex: components.InputSnapshot{ID: hitFindColorHex, Label: m.FieldColorCustom, Value: p.hex.String(),
			Focused: focused, PlatformEditing: focused && p.hex.String() != "", Invalid: p.invalid},
		Viewport: state.windowRect(), Layer: layer, Scope: hitFindColorDismiss,
	}
	if open && p.invalid {
		o.Error = m.ColorPickerInvalid
	}
	return o
}

// declareFindPreference declares one of the find preference controls in a row of
// the page: the two color triggers keep Viva's color picker (the documented
// canvas-level exception) in a slot, while the opacity and the origin are page
// controls.
func (state *State) declareFindPreference(c ui.C, id gfx.ID) ui.Box {
	v, m := &state.findSettingsUI, state.messages.FindSettings
	switch id {
	case hitFindForeground, hitFindBackground:
		row := ui.Row(c).Grow(1).Gap(8).Items(ui.Center)
		row.Children(func() {
			options := state.findColorOptions(c.Canvas(), id, 0)
			var hex [9]byte
			ui.Text(c, string(options.Value.AppendHex(hex[:0], false))).FontSize(metrics.FontSizeS).Mono().
				Color(state.pal.muted).Grow(1).Ellipsis()
			slot := ui.Slot(c).Size(34, 34).Shrink(0)
			slot.Draw(func(d *ui.DrawContext) {
				if id == hitFindForeground {
					v.foreground = d.Rect
				} else {
					v.background = d.Rect
				}
				components.BeginColorPicker(d.Canvas, *d.Theme, d.Rect, state.findColorOptions(d.Canvas, id, 0)).PaintTrigger()
			})
		})
		return row
	case hitFindSettingsOpacity:
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
	case hitFindSettingsOrigin:
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
	preview := state.declareFindPreview(c)
	return preview
}

// declareFindPreview declares the strip that shows the highlight colors, the
// opacity included, as they would read in the terminal.
func (state *State) declareFindPreview(c ui.C) ui.Box {
	preview := ui.Slot(c).Grow(1).Height(34)
	preview.Draw(func(d *ui.DrawContext) { state.paintFindPreview(d.Canvas, d.Rect) })
	return preview
}

func (state *State) paintFindPreferences(canvas *gfx.Canvas) {
	v := &state.findSettingsUI
	if !v.focus.IsZero() {
		canvas.RequestFocus(v.focus)
		v.focus = gfx.ID{}
	}
	bar := state.findSettingsOnBar()
	if !bar && !state.findSettingsOnPage() {
		v.close()
		return
	}

	// The color picker is the documented canvas-level exception: its panel is
	// painted over the page until a page form exists for it.
	if !v.color.IsZero() {
		anchor := v.foreground
		if v.color == hitFindBackground {
			anchor = v.background
		}
		if state.findSettingAnchorVisible(anchor) {
			v.picker.layout = components.BeginColorPicker(canvas, state.Theme(), anchor,
				state.findColorOptions(canvas, v.color, popupLayer+1)).PaintPanel()
		} else {
			v.color = gfx.ID{}
		}
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
			id    gfx.ID
			label string
		}{
			{hitFindForeground, m.Foreground}, {hitFindBackground, m.Background},
			{hitFindSettingsOpacity, m.Opacity}, {hitFindSettingsOrigin, m.Origin},
		} {
			ui.Row(c).Height(32).Gap(8).Items(ui.Center).Shrink(0).Children(func() {
				ui.Text(c, setting.label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.text).Width(144).Shrink(0).Ellipsis()
				state.declareFindPreference(c, setting.id)
			})
		}
		state.declareFindPreview(c).FillWidth().Height(32)
		ui.Button(c, m.Reset).Outline().Hit(hitFindSettingsReset, m.Reset).FillWidth().Shrink(0)
	})
	if dismissed {
		// The page closed the panel; the keyboard goes back to the find field,
		// as it does when the application's own dismiss handler closes it.
		c.After(func() {
			v.color = gfx.ID{}
			v.focus = hitTerminalFindField
			if session, ok := state.activeTerminalFind(); ok {
				v.focus = state.paneIdentity(session, hitTerminalFindField)
			}
		})
	}
}

func (state *State) findSettingAnchorVisible(anchor gfx.Rect) bool {
	top, bottom := windowBarHeight, state.viewport.Height
	if state.findSettingsOnPage() {
		viewport := state.settings.declared.scroll.Rect()
		top, bottom = max(top, viewport.Y), min(bottom, viewport.Y+viewport.Height)
	}
	return anchor.Width > 0 && anchor.Y >= top && anchor.Y+anchor.Height <= bottom
}

func (state *State) clickFindPreferences(id gfx.ID) bool {
	v := &state.findSettingsUI
	if !state.findSettingsOnBar() && !state.findSettingsOnPage() {
		return false
	}
	p := state.terminalFindPrefs
	switch id {
	case hitFindSettingsDismiss:
		v.close()
		v.focus = hitTerminalFindField
		if session, ok := state.activeTerminalFind(); ok {
			v.focus = state.paneIdentity(session, hitTerminalFindField)
		}
	case hitFindSettingsReset:
		state.setFindPreferences(defaultTerminalFindPreferences)
	case hitFindForeground, hitFindBackground:
		v.color = id
		value := p.Foreground
		if id == hitFindBackground {
			value = p.Background
		}
		color, _ := components.ParseColorHex(value, false)
		v.picker.setValue(components.ColorPickerValue{}.WithColor(color))
	case hitFindColorDismiss, hitFindColorCancel:
		v.focus, v.color = v.color, gfx.ID{}
	case hitFindColorApply, hitFindColorClear:
		if v.color.IsZero() || id == hitFindColorApply && v.picker.invalid {
			return true
		}
		value := string(v.picker.draft.AppendHex(nil, false))
		if v.color == hitFindForeground {
			if id == hitFindColorClear {
				value = defaultTerminalFindPreferences.Foreground
			}
			p.Foreground = value
		} else {
			if id == hitFindColorClear {
				value = defaultTerminalFindPreferences.Background
			}
			p.Background = value
		}
		state.setFindPreferences(p)
		v.focus, v.color = v.color, gfx.ID{}
	default:
		return false
	}
	return true
}

func (state *State) findPreferencesPointer(event gfx.PointerEvent) gfx.EventResult {
	v := &state.findSettingsUI
	if !state.findSettingsOnBar() && !state.findSettingsOnPage() {
		return gfx.EventResult{}
	}
	if !v.color.IsZero() {
		value, result := v.picker.layout.Pointer(event, v.picker.draft)
		if value != v.picker.draft {
			v.picker.setValue(value)
		}
		return result
	}
	return gfx.EventResult{}
}

func (state *State) findPreferencesKey(event gfx.KeyEvent) gfx.EventResult {
	v := &state.findSettingsUI
	if event.Release || !state.findSettingsOnBar() && !state.findSettingsOnPage() {
		return gfx.EventResult{}
	}
	// The popover, its select and its slider are page controls: the page routes
	// their keys. Only the color picker's panel is still painted by hand.
	if event.Key == gfx.KeyEscape && !v.color.IsZero() {
		if event.Target == hitFindColorHex {
			if _, composing := v.picker.hex.Composition(); composing {
				return gfx.EventResult{}
			}
		}
		state.clickFindPreferences(hitFindColorDismiss)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: v.focus, FocusVisible: true}
	}
	if !v.color.IsZero() {
		if event.Target == hitFindColorHex && event.Key == gfx.KeyEnter && event.Modifiers == 0 {
			if _, composing := v.picker.hex.Composition(); !composing {
				state.clickFindPreferences(hitFindColorApply)
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
		value, result := v.picker.layout.Key(event, v.picker.draft)
		if value != v.picker.draft {
			v.picker.setValue(value)
		}
		return result
	}
	return gfx.EventResult{}
}

func (state *State) findPreferencesEditor(id gfx.ID) *text.Editor {
	if id == hitFindColorHex && !state.findSettingsUI.color.IsZero() && (state.findSettingsOnPage() || state.findSettingsOnBar()) {
		return &state.findSettingsUI.picker.hex
	}
	return nil
}

// TerminalFindPreferencesInput connects the color picker's hex field to native editing.
func (state *State) TerminalFindPreferencesInput(context app.TextInputContext) *app.TextInput {
	editor := state.findPreferencesEditor(context.ID)
	if editor == nil {
		return nil
	}
	return &app.TextInput{
		State: app.TextStateOf(editor),
		OnEvent: func(event app.TextEvent) (app.TextState, error) {
			if state.findPreferencesEditor(context.ID) == editor {
				event.Apply(editor)
				state.findSettingsUI.picker.syncHex()
			}
			return app.TextStateOf(editor), nil
		},
	}
}

func (state *State) paintTerminalFindSettingsGroup(page *settingsPage) {
	m := state.messages.FindSettings
	page.group(m.Title, func() {
		c := page.c
		for _, setting := range [...]struct {
			id    gfx.ID
			label string
			width float32
		}{
			{hitFindForeground, m.Foreground, 104},
			{hitFindBackground, m.Background, 104},
			{hitFindSettingsOpacity, m.Opacity, 182},
			{hitFindSettingsOrigin, m.Origin, 192},
		} {
			page.lineFor(setting.id, setting.label, "", setting.width, func() {
				state.declareFindPreference(c, setting.id)
			})
		}
		page.lineWith(m.Preview, "", 192, func() { state.declareFindPreview(c) })
		page.actionRow(m.Title, "", false, components.ButtonAction{ID: hitFindSettingsReset, Label: m.Reset, Variant: components.ButtonOutline})
	})
}
