package workbench

import (
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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
	hitFindOriginDismiss   = tree.ID(findSettingsHits, "origin-dismiss")
	hitFindOriginItems     = tree.Sub(findSettingsHits, tree.Name("origins"))
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
	open, originOpen, opacityDrag          bool
	session                                uint32
	anchor, foreground, background, origin gfx.Rect
	color                                  gfx.ID
	picker                                 hostColorPicker
	slider                                 components.Slider
	focus                                  gfx.ID
}

func (v *terminalFindSettingsUI) close() {
	v.open, v.originOpen, v.opacityDrag = false, false, false
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

func (state *State) paintFindSetting(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect) {
	v, m := &state.findSettingsUI, state.messages.FindSettings
	switch id {
	case hitFindForeground, hitFindBackground:
		spot := gfx.Rect{X: bounds.X + bounds.Width - bounds.Height, Y: bounds.Y, Width: bounds.Height, Height: bounds.Height}
		if id == hitFindForeground {
			v.foreground = spot
		} else {
			v.background = spot
		}
		options := state.findColorOptions(canvas, id, 0)
		var hex [9]byte
		canvas.Label(string(options.Value.AppendHex(hex[:0], false)),
			gfx.Rect{X: bounds.X, Y: bounds.Y, Width: max(0, spot.X-bounds.X-8), Height: bounds.Height},
			gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted, Mono: true})
		components.BeginColorPicker(canvas, state.Theme(), spot, options).PaintTrigger()
	case hitFindSettingsOpacity:
		value := state.terminalFindPrefs.Opacity
		percent := strconv.Itoa(value) + "%"
		slider := bounds
		slider.Width = max(0, slider.Width-40)
		v.slider = components.BeginSlider(canvas, state.Theme(), id, slider, float32(value)).Range(0, 100).Step(1).ValueText(percent)
		v.slider.Paint(m.Opacity)
		canvas.LabelCenter(percent, gfx.Rect{X: bounds.X + slider.Width + 4, Y: bounds.Y, Width: 36, Height: bounds.Height},
			gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.muted})
	case hitFindSettingsOrigin:
		v.origin = bounds
		components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
			TriggerID: id, Label: m.Origin, Value: state.findOriginLabel(state.terminalFindPrefs.Origin),
		}).End()
	default:
		state.paintFindPreview(canvas, bounds)
	}
}

func (state *State) paintFindPreferences(canvas *gfx.Canvas) {
	v, m := &state.findSettingsUI, state.messages.FindSettings
	if !v.focus.IsZero() {
		canvas.RequestFocus(v.focus)
		v.focus = gfx.ID{}
	}
	bar := state.findSettingsOnBar()
	if !bar && !state.findSettingsOnPage() {
		v.close()
		return
	}

	if bar && v.anchor.Width > 0 {
		theme := state.Theme()
		height := theme.Metrics.ControlHeight
		const gap = float32(8)
		popover := components.BeginPopover(canvas, theme, state.windowRect(), v.anchor, components.PopoverOptions{
			Layer: popupLayer, Scope: hitFindSettingsDismiss, DismissID: hitFindSettingsDismiss,
			Align: components.PopoverAlignEnd, AnchorDismiss: true, ConsumeOutsidePress: true, SideOffset: 6,
			ContentSize: gfx.Size{Width: 370}, BodyHeight: 6*height + 5*gap,
		})
		body := popover.Begin(m.Title, "").Body
		canvas.TrapFocus(hitFindSettingsDismiss)
		canvas.Clip(body)
		for index, setting := range [...]struct {
			id    gfx.ID
			label string
		}{
			{hitFindForeground, m.Foreground}, {hitFindBackground, m.Background},
			{hitFindSettingsOpacity, m.Opacity}, {hitFindSettingsOrigin, m.Origin},
		} {
			y := body.Y + float32(index)*(height+gap)
			left := min(144, body.Width*0.48)
			label := gfx.Rect{X: body.X, Y: y, Width: max(0, left-8), Height: height}
			canvas.Label(elide(canvas, setting.label, metrics.FontSizeS, gfx.WeightMedium, label.Width), label,
				gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.text})
			state.paintFindSetting(canvas, setting.id, gfx.Rect{X: body.X + left, Y: y, Width: max(0, body.Width-left), Height: height})
		}
		preview := gfx.Rect{X: body.X, Y: body.Y + 4*(height+gap), Width: body.Width, Height: height}
		state.paintFindPreview(canvas, preview)
		reset := gfx.Rect{X: body.X, Y: preview.Y + height + gap, Width: body.Width, Height: height}
		components.BeginButton(canvas, theme, hitFindSettingsReset, reset).Outline(m.Reset)
		canvas.Unclip()
		popover.End()
	}

	if v.originOpen {
		anchor := v.origin
		if state.findSettingAnchorVisible(anchor) {
			layer := popupLayer + 1
			content := state.popupAt(canvas, state.windowRect(), gfx.Point{X: anchor.X, Y: anchor.Y + anchor.Height + 4},
				max(180, anchor.Width), 152, 4)
			canvas.PushLayer(layer)
			control := components.BeginSelect(canvas, state.Theme(), anchor, components.SelectOptions{
				TriggerID: hitFindSettingsOrigin, Label: m.Origin, Value: state.findOriginLabel(state.terminalFindPrefs.Origin),
				Open: true, Layer: layer, Scope: hitFindOriginDismiss, DismissID: hitFindOriginDismiss,
				Content: content, Viewport: state.windowRect(),
			})
			for index, origin := range terminalFindOrigins {
				control.AddItem(components.SelectItem{ID: hitFindOriginItems.At(index), Label: state.findOriginLabel(origin),
					Selected: state.terminalFindPrefs.Origin == origin})
			}
			control.End()
			canvas.PopLayer()
		} else {
			v.originOpen = false
		}
	}

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
	case hitFindSettingsOrigin:
		v.originOpen = !v.originOpen
		v.color = gfx.ID{}
		if v.originOpen {
			for index, origin := range terminalFindOrigins {
				if p.Origin == origin {
					v.focus = hitFindOriginItems.At(index)
				}
			}
		}
	case hitFindOriginDismiss:
		v.originOpen, v.focus = false, hitFindSettingsOrigin
	case hitFindForeground, hitFindBackground:
		v.color = id
		v.originOpen = false
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
		index, ok := id.In(hitFindOriginItems)
		if !ok || index < 0 || index >= len(terminalFindOrigins) {
			return false
		}
		p.Origin = terminalFindOrigins[index]
		state.setFindPreferences(p)
		v.originOpen, v.focus = false, hitFindSettingsOrigin
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
	if v.opacityDrag {
		switch event.Kind {
		case gfx.PointerMove:
			p := state.terminalFindPrefs
			p.Opacity = int(v.slider.ValueAt(event.X))
			state.applyFindPreferences(p)
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitFindSettingsOpacity}
		case gfx.PointerUp, gfx.PointerExit:
			v.opacityDrag = false
			state.save()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitFindSettingsOpacity}
		}
		// A press elsewhere ends a drag whose release was never delivered; that
		// press still reaches what it names.
		v.opacityDrag = false
		state.save()
	}
	if event.Target != hitFindSettingsOpacity {
		return gfx.EventResult{}
	}
	switch {
	case event.Kind == gfx.PointerDown && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone):
		p := state.terminalFindPrefs
		p.Opacity = int(v.slider.ValueAt(event.X))
		state.applyFindPreferences(p)
		v.opacityDrag = true
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitFindSettingsOpacity}
	}
	return gfx.EventResult{}
}

func (state *State) findPreferencesKey(event gfx.KeyEvent) gfx.EventResult {
	v := &state.findSettingsUI
	if event.Release || !state.findSettingsOnBar() && !state.findSettingsOnPage() {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape && (v.open || v.originOpen || !v.color.IsZero()) {
		if event.Target == hitFindColorHex {
			if _, composing := v.picker.hex.Composition(); composing {
				return gfx.EventResult{}
			}
		}
		switch {
		case !v.color.IsZero():
			state.clickFindPreferences(hitFindColorDismiss)
		case v.originOpen:
			state.clickFindPreferences(hitFindOriginDismiss)
		default:
			state.clickFindPreferences(hitFindSettingsDismiss)
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: v.focus, FocusVisible: true}
	}
	if v.originOpen {
		switch event.Key {
		case gfx.KeyUp, gfx.KeyDown, gfx.KeyHome, gfx.KeyEnd:
			index, ok := event.Target.In(hitFindOriginItems)
			if !ok {
				index = -1
			}
			count := len(terminalFindOrigins)
			switch event.Key {
			case gfx.KeyHome:
				index = 0
			case gfx.KeyEnd:
				index = count - 1
			case gfx.KeyUp:
				if index < 0 {
					index = 0
				}
				index = (index + count - 1) % count
			case gfx.KeyDown:
				index = (index + 1) % count
			}
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitFindOriginItems.At(index), FocusVisible: true}
		}
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
	if event.Target == hitFindSettingsOpacity {
		p := state.terminalFindPrefs
		value, handled := components.SliderKeyDown(float32(p.Opacity), 0, 100, 1, components.LTR, event.Key, event.Modifiers)
		if handled {
			p.Opacity = int(value)
			state.setFindPreferences(p)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
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
			slot := page.lineFor(setting.id, setting.label, "", setting.width, nil)
			page.add(settingsControl{kind: controlFindPreference, slot: slot, id: setting.id})
		}
		page.add(settingsControl{kind: controlFindPreference, slot: page.line(m.Preview, "", 192)})
		page.actionRow(m.Title, "", false, components.ButtonAction{ID: hitFindSettingsReset, Label: m.Reset, Variant: components.ButtonOutline})
	})
}
