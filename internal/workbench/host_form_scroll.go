package workbench

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var hostFormHits = tree.Feature("host-form")
var hitHostFormViewport = tree.ID(hostFormHits, "viewport")
var hitHostFormScrollbar = tree.ID(hostFormHits, "scrollbar")

type hostFormScroll struct {
	offset, grab float32
	// pageScroll is the body's page scroll area as of this paint: the form reads
	// and moves its offset instead of keeping a scroll of its own. viewport is
	// where the body lies, for the pickers' clip and the tab ring's reveal.
	pageScroll *ui.ScrollState
	// scroller is the body's page scroll area, read after layout for the
	// viewport the pickers clip to and the tab ring reveals inside.
	scroller          ui.Box
	viewport          gfx.Rect
	maxOffset         float32
	dragging, clipped bool
	lastFocus         gfx.ID
	lastError         string
	stops             [96]hostFormStop
	count             int
	// body is the slots the page laid out for the controls painted by hand.
	body hostFormBody
	// fieldLabel, fieldAddress, fieldUser, fieldPassphrase and fieldPort are the
	// endpoint text fields the page declares; fieldIdleInterval and
	// fieldIdleCustomInput are the probe's custom values.
	fieldLabel, fieldAddress, fieldUser, fieldPassphrase, fieldPort dialogField
	fieldIdleInterval, fieldIdleCustomInput                         dialogField
	fieldAlgoCustom, fieldLoginCommand, fieldOTP                    dialogField
	fieldResolution                                                 dialogField
	wslUserField, wslDirectoryField, wslCommandField                dialogField
	wslDistroField                                                  ui.FormFieldState
	wslDistroBox                                                    ui.Box
	wslDistroChoices                                                []string
	// authField and portsField are the page form fields of the authentication
	// choice and the detected serial ports; authBox is the choice itself, kept
	// for the form's tab ring.
	authField, portsField ui.FormFieldState
	authBox, portsBox     ui.Box
	// baudField and baudBox are the serial line's common-rate select, baudChoice
	// the rate it shows, and baudChoices the options, rebuilt each paint.
	baudField   ui.FormFieldState
	baudBox     ui.Box
	baudChoice  string
	baudChoices []string
	// encodingField is the remote-encoding combobox's form field, encodingBox
	// the combobox itself and encodingChoice the value it shows.
	encodingField  ui.FormFieldState
	encodingBox    ui.Box
	encodingChoice string
	// iconField, iconBox and iconChoice are the icon select, and
	// iconChoices/iconIcons the options and their art, rebuilt each paint.
	iconField   ui.FormFieldState
	iconBox     ui.Box
	iconChoice  hostIcon
	iconChoices []hostIcon
	iconIcons   []gfx.Icon
	portChoice  string
	portChoices []string
	// idleInputChoice is the value the input select shows: the editor's own
	// choice, or the protocol's default while none was made.
	idleInputChoice    idleInputMode
	idleIntervalChoice idleKeepAlivePreset
	idleUnitChoice     idleIntervalUnit
	idleIntervalBox    ui.Box
	idleUnitBox        ui.Box
	idleInputBox       ui.Box
	idleIntervalField  ui.FormFieldState
	idleUnitField      ui.FormFieldState
	idleInputField     ui.FormFieldState
	// proxyField and jumpField are the Outbound proxy and Jump route rows the
	// page declares as form fields; each keeps its own measured layout between
	// paints.
	proxyField, jumpField ui.FormFieldState
	// colorField is the colour row's page form field and colorControl the slot
	// its swatches and picker are painted in.
	colorField   ui.FormFieldState
	colorControl ui.Box
	// passwordField is the shared-password field's form row; passwordControl is
	// where its hand-painted addons and text are painted.
	passwordField   ui.FormFieldState
	passwordControl ui.Box
	// colorValue is the picker's staged colour, colorState its open panel, and
	// colorSwatchBoxes/colorPickerBox/randomColorBox the page controls for the tab ring.
	colorValue       gfx.Color
	colorState       ui.ColorPickerState
	colorSwatchBoxes [len(hostColors)]ui.Box
	colorPickerBox   ui.Box
	randomColorBox ui.Box
	// algoLayout is the algorithm mode this frame's advanced rows were laid out
	// for, before the page's choice writes the mode a click chose.
	algoLayout algoMode
	// layoutProtocol is the protocol this frame's body was laid out for, before
	// the page's choice writes the protocol a click chose.
	layoutProtocol hostProtocol
	// choiceIDs are the last painted option of each page Choice, kept so the
	// form's tab ring can reach a choice while it lies outside the viewport.
	choiceIDs hostFormChoiceIDs
	// vncBody, algoBody and idleBody are the slots the sections painted inside
	// the form laid out.
	vncBody  hostVNCRowsBody
	algoBody hostAlgoRowsBody
	idleBody hostIdleKeepAliveRowsBody
	// qualityBody is the quality section of a desktop host.
	qualityBody hostQualityBody
	// resolutionBody is the resolution rows of an RDP host.
	resolutionBody hostResolutionBody
	// idleLayout and idleInputLayout are the probe values this frame's rows were
	// laid out for, before a click writes the values the next frame shows.
	idleLayout      idleKeepAlivePreset
	idleInputLayout idleInputMode
}

// hostFormChoiceIDs is the chosen option hit of each Choice of the host form.
type hostFormChoiceIDs struct {
	protocol, flow, backspace, algo, transfer, loginPolicy gfx.ID
}

// hostFormBody is where the host form's body put each control: the slots it
// paints by hand and the Box each page Choice was declared in.
type hostFormBody struct {
	protocol, label, endpoint, group, tags, encoding       ui.Box
	iconField, colorField, transfer, proxy, jump, advanced ui.Box
	vncRows                                                ui.Box
	flow, algo                                             ui.Box
	backspace                                              ui.Box
	// proxyRow and jumpRow are the controls the proxy and jump fields place,
	// and proxyAction and jumpAction the slots their buttons are painted in.
	proxyRow, jumpRow       ui.Box
	proxyAction, jumpAction ui.Box
	// endpointSlots are the rows the endpoint block declared for its fields.
	endpointSlots hostEndpointSlots
	// advancedHeader and advancedHeaderIcon are the row that opens the Advanced
	// section and the chevron it paints, both declared by the page.
	advancedHeader, advancedHeaderIcon ui.Box
	// fields are the page's text fields of this frame, for the tab ring: a control
	// outside the clipped viewport is still reachable from the keyboard.
	fields     [10]hostFormFieldRef
	fieldCount int
}

// hostFormFieldRef ties a page field's identity to the field declared for it.
type hostFormFieldRef struct {
	id    gfx.ID
	field *dialogField
}

// addField remembers a page field declared by this frame's body.
func (b *hostFormBody) addField(id gfx.ID, field *dialogField) {
	if b.fieldCount == len(b.fields) {
		return
	}
	b.fields[b.fieldCount] = hostFormFieldRef{id: id, field: field}
	b.fieldCount++
}

// hostEndpointSlots is the endpoint block's declared geometry, by the role the
// painter gives each slot. A role the current protocol does not use aliases a
// slot that exists, so the event pass of a protocol change can still paint the
// protocol now in effect without a box of its own.
type hostEndpointSlots struct {
	address, picker, second, password, third, authentication, identity, passphrase ui.Box
}

type hostFormStop struct {
	id gfx.ID
	// focus is the identity the stop's control paints, when it paints one of
	// its own: a page Select's trigger. The ring keeps the application's id for
	// the handler and moves the keyboard to this one.
	focus  gfx.ID
	bounds gfx.Rect
	body   bool
}

func (f *hostFormScroll) addStop(id gfx.ID, bounds gfx.Rect, body bool) {
	if id.IsZero() || f.count == len(f.stops) {
		return
	}
	for i := 0; i < f.count; i++ {
		if f.stops[i].id == id {
			return
		}
	}
	at := f.count
	if body {
		for at > 0 {
			p := f.stops[at-1]
			if p.body && (p.bounds.Y < bounds.Y || p.bounds.Y == bounds.Y && p.bounds.X <= bounds.X) {
				break
			}
			f.stops[at] = p
			at--
		}
	}
	f.stops[at] = hostFormStop{id: id, focus: id, bounds: bounds, body: body}
	f.count++
}

// Keep logical tab order even when a control lies outside the clipped viewport.
func (state *State) rememberHostControl(id gfx.ID, bounds gfx.Rect) {
	if state.editor.form.clipped {
		state.editor.form.addStop(id, bounds, true)
	}
}

// rememberHostChoice keeps a page Choice in the form's tab ring. The choice
// paints its options with identities of its own, so take the hit of the chosen
// option from the paint; its identity is kept while the choice lies outside the
// clipped viewport, where the paint no longer reports it.
func (state *State) rememberHostChoice(canvas *gfx.Canvas, bounds gfx.Rect, chosen *gfx.ID, label string) {
	for _, hit := range canvas.Hits() {
		if hit.Label != label || hit.Role != gfx.RoleRadio {
			continue
		}
		if hit.Bounds.X >= bounds.X && hit.Bounds.Y >= bounds.Y &&
			hit.Bounds.X < bounds.X+bounds.Width && hit.Bounds.Y < bounds.Y+bounds.Height {
			*chosen = hit.ID
			break
		}
	}
	if !chosen.IsZero() {
		state.rememberHostControl(*chosen, bounds)
	}
}

func (state *State) hostFormPopupOpen() bool {
	e := &state.editor
	return e.identityOpen || e.credentialOpen ||
		e.groupPicker.control.Open || e.tagPicker.control.Open
}

// scrollHostForm gives the wheel to the body's page scroll area, whose offset the
// page keeps, or to an open page select over it. The dialog owns the wheel over
// its backdrop: a wheel the body cannot use goes nowhere else, and the page's
// own routing keeps the same event from falling back to the window's handler.
func (state *State) scrollHostForm(event gfx.ScrollEvent) gfx.EventResult {
	if !state.hostFormPageListOpen() && !hostFormContains(state.editor.form.viewport, event.X, event.Y) {
		return gfx.EventResult{Handled: true}
	}
	result := state.windowPage().RouteWheel(event)
	result.Handled = true
	return result
}

// hostFormPageListOpen reports whether a page select or combobox of the form has
// its list open: the open list owns the wheel wherever the pointer is.
func (state *State) hostFormPageListOpen() bool {
	f := &state.editor.form
	for _, box := range [...]ui.Box{f.iconBox, f.authBox, f.portsBox, f.idleIntervalBox, f.idleUnitBox, f.idleInputBox, f.qualityBody.depthBox, f.qualityBody.encodingBox, f.resolutionBody.box} {
		if !box.Valid() {
			continue
		}
		if _, open := box.Popup(); open {
			return true
		}
	}
	if f.encodingBox.Valid() {
		if combo := f.encodingBox.ComboState(); combo != nil && combo.GetList().Open {
			return true
		}
	}
	return false
}

func hostFormContains(b gfx.Rect, x, y float32) bool {
	return x >= b.X && y >= b.Y && x < b.X+b.Width && y < b.Y+b.Height
}

func (state *State) hostFormKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalNewHost || state.hostFormPopupOpen() || event.Release {
		return gfx.EventResult{}
	}
	f := &state.editor.form
	old := f.offset
	if event.Key == gfx.KeyTab && event.Modifiers & ^gfx.ModifierShift == 0 && f.count > 0 {
		at := -1
		for i := 0; i < f.count; i++ {
			if f.stops[i].id == event.Target || f.stops[i].focus == event.Target {
				at = i
				break
			}
		}
		if at < 0 && event.Target != hitHostFormViewport {
			return gfx.EventResult{}
		}
		if event.Modifiers&gfx.ModifierShift != 0 {
			if at < 0 {
				at = 0
			}
			at = (at - 1 + f.count) % f.count
		} else {
			at = (at + 1) % f.count
		}
		next := f.stops[at]
		if next.body {
			f.reveal(next.bounds)
		}
		focus := next.focus
		if focus.IsZero() {
			focus = next.id
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: focus, FocusVisible: true}
	}
	if event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyPageDown:
		f.scrollBy(f.viewportHeight() * .9)
	case gfx.KeyPageUp:
		f.scrollBy(-f.viewportHeight() * .9)
	case gfx.KeyHome:
		if event.Target != hitHostFormViewport {
			return gfx.EventResult{}
		}
		f.scrollTo(0)
	case gfx.KeyEnd:
		if event.Target != hitHostFormViewport {
			return gfx.EventResult{}
		}
		f.scrollTo(f.maxOffset)
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: f.offset != old}
}

// viewportHeight is the height of the body's scroll area this paint.
func (f *hostFormScroll) viewportHeight() float32 {
	if f.pageScroll != nil {
		return f.pageScroll.Viewport
	}
	return f.viewport.Height
}

// scrollTo moves the body to offset, clamped to its content.
func (f *hostFormScroll) scrollTo(offset float32) {
	offset = max(0, min(f.maxOffset, offset))
	f.offset = offset
	if f.pageScroll != nil {
		f.pageScroll.ScrollTo(offset)
	}
}

func (f *hostFormScroll) scrollBy(delta float32) { f.scrollTo(f.offset + delta) }

// rememberHostPageSelect keeps a page Select or Combobox in the form's tab ring
// by the identity the application gave it. Once the row has been scrolled into
// view the component has painted its own trigger, and the focus moves there so
// the keyboard opens it the way the page's own routing expects.
func (state *State) rememberHostPageSelect(canvas *gfx.Canvas, box ui.Box, id gfx.ID) {
	if !box.Valid() {
		return
	}
	rect := box.Rect()
	state.rememberHostControl(id, rect)
	for _, hit := range canvas.Hits() {
		if hit.Role != gfx.RoleCombobox {
			continue
		}
		if hit.Bounds.X >= rect.X && hit.Bounds.Y >= rect.Y &&
			hit.Bounds.X < rect.X+rect.Width && hit.Bounds.Y < rect.Y+rect.Height {
			f := &state.editor.form
			for i := 0; i < f.count; i++ {
				if f.stops[i].id == id {
					f.stops[i].focus = hit.ID
					break
				}
			}
			if canvas.Focused() == id {
				canvas.RequestFocus(hit.ID)
			}
			return
		}
	}
}

func (state *State) revealHostField(canvas *gfx.Canvas, id gfx.ID, bounds gfx.Rect) {
	f := &state.editor.form
	state.rememberHostControl(id, bounds)
	if !f.clipped || canvas.Focused() != id || f.lastFocus == id {
		return
	}
	f.lastFocus = id
	old := f.offset
	f.reveal(bounds)
	if f.offset != old {
		canvas.RequestFrameIn(time.Nanosecond)
	}
}

func (f *hostFormScroll) reveal(bounds gfx.Rect) {
	// Include the label above the control, not only the caret's line.
	view := f.viewport
	top := bounds.Y - (metrics.FontSizeM + 4) - metrics.GapItem/2
	offset := f.offset
	if top < view.Y {
		offset += top - view.Y
	} else if bottom := bounds.Y + bounds.Height; bottom > view.Y+view.Height {
		offset += bottom - view.Y - view.Height
	}
	f.scrollTo(offset)
}

// Host selectors paint while the body clip is open. Their popups belong to the
// window; release only the clip owned by this form, then restore it afterwards.
func (state *State) beginHostFormPopup(canvas *gfx.Canvas, open bool) bool {
	if open && state.editor.form.clipped {
		canvas.Unclip()
		return true
	}
	return false
}
func (state *State) endHostFormPopup(canvas *gfx.Canvas, portal bool) {
	if portal {
		canvas.Clip(state.editor.form.viewport)
	}
}
