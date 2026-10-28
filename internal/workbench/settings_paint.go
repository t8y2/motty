package workbench

import (
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

// Settings are laid out as titled groups: one card per group, its rows
// separated by hairlines, in a centered column that scrolls when the window is
// shorter than the page.
const (
	settingsColumnMax = float32(760)
	settingsRowHeight = float32(64)
	settingsGroupGap  = float32(24)
	settingsDescLines = 2
)

// settingsControlKind is how a recorded control is painted.
type settingsControlKind uint8

const (
	controlSegments       settingsControlKind = iota // a segmented choice
	controlSwitch                                    // a two-state switch
	controlButtons                                   // buttons against the right edge of the slot
	controlButton                                    // one small button filling the slot
	controlHit                                       // the slot is a pointer target
	controlSearch                                    // the search box
	controlModelInput                                // the assistant's model-name field
	controlSwatches                                  // a strip of round color dots
	controlLabel                                     // the words of a row, which label the control beside them
	controlMCPSelect                                 // client configuration select
	controlLogo                                      // the product wordmark in a slot
	controlLanguageSelect                            // the application language select
)

// settingsControl is one control of the page, recorded while the page is
// declared and painted into its slot once the page has been laid out: a flex
// frame paints when it ends, and the keyboard visits controls in the order they
// are painted, so everything interactive is painted here, in the order the page
// declares it. The arrays are copies, so a record holds no pointer into the
// section that made it, and the records live in slices the page reuses.
type settingsControl struct {
	kind     settingsControlKind
	slot     flex.Box
	id       ui.ID
	label    string
	selected ui.ID
	on       bool
	disabled bool
	style    aiButtonStyle
	hit      ui.HitOptions
	// first and count name the control's options or actions in the page's arenas.
	first, count int
}

// settingsPage declares the settings page into a flex frame. The sections call
// group and the row methods inside it; every control they name is recorded in the
// state and painted by paintSettingsControls after the frame has ended.
type settingsPage struct {
	state *State
	f     *flex.Frame
	// width is the width of a group's card.
	width float32
	// rowHeight is the height of the rows of the groups being declared.
	rowHeight float32
	// controlWidth, when set, is the width of the buttons of an action row.
	controlWidth float32
	rows         int // rows declared in the current card
	opened       int // groups declared so far
	// navList is the scrolling column of categories, whose height and content
	// say how far the list can be scrolled.
	navList flex.Box

	// anchor is what the search result being opened asked to scroll to, once the
	// declaration has met it; anchorShift is the room above it that the scroll
	// leaves.
	anchor      flex.Box
	anchored    bool
	anchorShift float32
}

// The room above the page's title and below its last group, which scroll with
// the page.
const (
	settingsPageTop    = float32(34)
	settingsPageBottom = float32(28)
)

func (state *State) paintSettingsWorkspace(canvas *ui.Canvas, bounds ui.Rect) {
	pal, view := state.pal, &state.settings
	navWidth := min(settingsNavWidthMax, max(bounds.Width*0.26, 150))
	areaWidth := max(bounds.Width-navWidth-1, 0)
	maxScroll := max(state.settingsContent-bounds.Height, 0)
	state.settingsScroll = min(max(state.settingsScroll, 0), maxScroll)
	offset := state.settingsScroll
	// The category list keeps its own offset, clamped to what the last paint
	// said it could hold.
	view.navScroll = min(max(view.navScroll, 0), max(view.navContent-view.navView, 0))

	title := state.settingsCategoryName(state.settingsCategory)
	searching := state.settingsSearching()
	if searching {
		title = state.messages.Tools.SearchTitle
	}

	view.controls, view.options, view.actions, view.colors = view.controls[:0], view.options[:0], view.actions[:0], view.colors[:0]
	f := state.settingsPage.Begin(canvas, state.Theme(), bounds)
	page := settingsPage{state: state, f: f, width: min(settingsColumnMax, max(areaWidth-64, 0)), rowHeight: settingsRowHeight}
	var scroll flex.Box
	f.Row().Fill().Children(func() {
		page.nav(navWidth, searching)
		f.Column().Width(1).FillHeight().Background(withAlpha(pal.line, 0.8))
		scroll = f.Scroll(offset).Grow(1).Basis(0).Items(flex.Center).Children(func() {
			f.Column().FillWidth().MaxWidth(settingsColumnMax).MarginSides(settingsPageTop, 32, settingsPageBottom, 32).Children(func() {
				f.Text(title).FontSize(26).Bold().Color(pal.text).LineHeight(34).MarginSides(0, 0, 20, 0)
				if searching {
					state.paintSettingsResults(&page)
				} else {
					state.paintSettingsCategory(&page)
				}
			})
		})
	})
	f.End()
	state.settingsContent = scroll.Content()
	view.navContent, view.navView = page.navList.Content(), page.navList.Rect().Height
	view.navRect = page.navList.Rect()
	if clamped := min(max(view.navScroll, 0), max(view.navContent-view.navView, 0)); clamped != view.navScroll {
		view.navScroll = clamped
		canvas.RequestFrame()
	}
	state.paintSettingsControls(canvas)

	if page.anchored {
		// Asked to scroll to a setting: it is known where it is now that the page
		// is laid out; one more paint shows the page there.
		contentY := page.anchor.Rect().Y - scroll.Rect().Y + offset
		state.settingsScroll = max(contentY-2*settingsPageTop-8-page.anchorShift, 0)
		view.jumped = true
	}
	if view.jumped {
		view.jumped = false
		canvas.RequestFrame()
	}
}

// paintSettingsControls paints the controls the page recorded into their slots,
// in the order they were declared.
func (state *State) paintSettingsControls(canvas *ui.Canvas) {
	theme := state.Theme()
	for index := range state.settings.controls {
		control := &state.settings.controls[index]
		rect := control.slot.Rect()
		switch control.kind {
		case controlSegments:
			components.BeginSegmentedControl(canvas, theme, rect, state.settings.options[control.first:control.first+control.count], control.selected).Paint()
		case controlMCPSelect:
			state.ai.cfg.mcpSelectBounds = rect
		case controlLanguageSelect:
			state.languagePicker.bounds = rect
		case controlSwitch:
			state.aiSwitch(canvas, control.id, rect, control.on, control.disabled, control.label)
		case controlButtons:
			row := components.BeginButtonRow(canvas, theme, rect)
			if control.disabled {
				row = row.Disabled()
			}
			row.Paint(state.settings.actions[control.first : control.first+control.count]...)
		case controlButton:
			state.aiButton(canvas, control.id, rect, control.label, control.style, control.disabled)
		case controlHit:
			canvas.HitWithOptions(control.id, rect, control.label, control.hit)
		case controlLabel:
			canvas.HitLabel(control.id, rect, control.label, control.disabled)
		case controlSearch:
			state.paintSettingsSearchBox(canvas, rect)
		case controlModelInput:
			state.paintAIModelInput(canvas, rect)
		case controlSwatches:
			// The dots are the drawing: a strip of circles a pitch apart.
			for slot, color := range state.settings.colors[control.first : control.first+control.count] {
				canvas.Rounded(ui.Rect{X: rect.X + float32(slot)*swatchPitch, Y: rect.Y, Width: swatchSize, Height: swatchSize}, swatchSize/2, color)
			}
		case controlLogo:
			state.paintLockup(canvas, rect)
		}
	}
}

func (state *State) paintSettingsCategory(page *settingsPage) {
	switch state.settingsCategory {
	case settingsGeneral:
		state.paintGeneralSettings(page)
	case settingsTerminal:
		state.paintTerminalSettings(page)
	case settingsConnection:
		state.paintConnectionSettings(page)
	case settingsFiles:
		state.paintFilesSettings(page)
	case settingsSecurity:
		state.paintSecuritySettings(page)
	case settingsBackup:
		state.paintBackupSettings(page)
	case settingsAssistant:
		state.paintAISettings(page)
	case settingsMCP:
		state.paintMCPSettings(page)
	case settingsShortcuts:
		state.paintShortcutSettings(page)
	case settingsAbout:
		state.paintAboutSettings(page)
	}
}

func (state *State) paintGeneralSettings(page *settingsPage) {
	messages := state.messages
	appearance := state.settingsAppearanceOptions()
	selectedAppearance := hitSettingsAppearanceSystem
	switch state.appearanceMode {
	case appearancePinnedDark:
		selectedAppearance = hitSettingsAppearanceDark
	case appearancePinnedLight:
		selectedAppearance = hitSettingsAppearanceLight
	}
	page.groups(func() {
		page.group("", func() {
			page.add(settingsControl{kind: controlLanguageSelect, slot: page.rowFrameFor(messages.SettingsLanguage, "", 240)})
			page.row(messages.SettingsAppearance, "", appearance[:], selectedAppearance)
			if state.options.StartAtLogin != nil {
				state.paintLoginSetting(page)
			}
		})
		state.paintUpdateSettings(page)
	})
}

func (state *State) paintTerminalSettings(page *settingsPage) {
	messages := state.messages
	option := state.settingsOptionOptions()
	selectedOption := hitSettingsOptionText
	if state.terminalOption == terminalOptionMeta {
		selectedOption = hitSettingsOptionMeta
	}
	padding := state.settingsPaddingOptions()
	selectedPadding := hitSettingsPadNormal
	switch state.terminalPad {
	case padCompact:
		selectedPadding = hitSettingsPadCompact
	case padRoomy:
		selectedPadding = hitSettingsPadRoomy
	}
	gutter := state.settingsGutterOptions()
	selectedGutter := [...]ui.ID{hitSettingsGutterOff, hitSettingsGutterNumbers, hitSettingsGutterTime, hitSettingsGutterBoth}[state.terminalGutter]
	scrollback := state.settingsScrollbackOptions()
	selectedScrollback := hitSettingsScrollback5000
	switch state.terminalScrollback {
	case 1000:
		selectedScrollback = hitSettingsScrollback1000
	case 10000:
		selectedScrollback = hitSettingsScrollback10000
	}
	clipboard := state.settingsClipboardOptions()
	selectedClipboard := hitSettingsClipboardAsk
	switch state.terminalClipboard {
	case terminalClipboardBlock:
		selectedClipboard = hitSettingsClipboardBlock
	case terminalClipboardAllow:
		selectedClipboard = hitSettingsClipboardAllow
	}
	page.groups(func() {
		page.group("", func() {
			page.row(messages.SettingsTerminalOption, messages.SettingsTerminalOptionDescription, option[:], selectedOption)
			page.actionRow(messages.SettingsTerminalFontSize, messages.SettingsTerminalFontSizeDescription, false,
				components.ButtonAction{ID: hitSettingsFontSizeDown, Label: "−", Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitSettingsFontSizeReset, Label: state.fontSizeLabel(), Variant: components.ButtonGhost},
				components.ButtonAction{ID: hitSettingsFontSizeUp, Label: "+", Variant: components.ButtonOutline})
			page.actionRow(messages.SettingsTerminalFontFamily, state.fontDescription(), false,
				components.ButtonAction{ID: hitSettingsFontPick, Label: state.messages.Tools.FontChoose, Variant: components.ButtonOutline})
			page.row(state.messages.Tools.PaddingRow, "", padding[:], selectedPadding)
			page.row(state.messages.Tools.GutterRow, state.messages.Tools.GutterDescription, gutter[:], selectedGutter)
			page.toggleRow(state.messages.Tools.CursorRow, state.messages.Tools.CursorDescription, !state.terminalCursorSteady, hitSettingsCursorToggle)
			page.row(messages.SettingsTerminalScrollback, messages.SettingsTerminalScrollbackDescription, scrollback[:], selectedScrollback)
			page.row(messages.SettingsTerminalClipboard, messages.SettingsTerminalClipboardDescription, clipboard[:], selectedClipboard)
			page.toggleRow(state.messages.Tools.NotifyRow, state.messages.Tools.NotifyDescription, !state.notifyOff, hitSettingsNotifyToggle)
			page.toggleRow(state.messages.History.SettingsRow, state.messages.History.SettingsDescription, state.history.on, hitSettingsHistoryToggle)
			page.toggleRow(state.messages.History.SuggestRow, state.messages.History.SuggestDescription, !state.history.suggestOff, hitSettingsSuggestToggle)
			page.actionRow(state.messages.History.ClearRow, state.messages.History.ClearDescription, false,
				components.ButtonAction{ID: hitSettingsHistoryClear, Label: state.messages.History.Clear, Variant: components.ButtonOutline})
		})
		state.paintSchemeSettings(page)
		state.paintHighlightSettings(page)
		state.paintAutofillSettings(page)
		state.paintSnippetSettings(page)
		state.paintBackgroundSettings(page)
	})
}

func (state *State) paintConnectionSettings(page *settingsPage) {
	messages := state.messages
	timeout := state.settingsTimeoutOptions()
	selectedTimeout := hitSettingsTimeout10
	switch state.connectionTimeout {
	case 5 * time.Second:
		selectedTimeout = hitSettingsTimeout5
	case 30 * time.Second:
		selectedTimeout = hitSettingsTimeout30
	}
	keepAlive := state.settingsKeepAliveOptions()
	selectedKeepAlive := hitSettingsKeepAliveOff
	switch state.connectionKeepAlive {
	case 30 * time.Second:
		selectedKeepAlive = hitSettingsKeepAlive30
	case 60 * time.Second:
		selectedKeepAlive = hitSettingsKeepAlive60
	}
	reconnect := state.settingsReconnectOptions()
	selectedReconnect := hitSettingsReconnectOff
	switch state.connectionReconnect {
	case reconnectThree:
		selectedReconnect = hitSettingsReconnectThree
	case reconnectForever:
		selectedReconnect = hitSettingsReconnectForever
	}
	restore := state.settingsRestoreOptions()
	selectedRestore := hitSettingsRestoreOff
	switch state.restoreConnect {
	case restoreConnectActive:
		selectedRestore = hitSettingsRestoreActive
	case restoreConnectAll:
		selectedRestore = hitSettingsRestoreAll
	}
	page.groups(func() {
		page.group("", func() {
			page.row(messages.SettingsConnectionTimeout, messages.SettingsConnectionTimeoutDescription, timeout[:], selectedTimeout)
			page.row(messages.SettingsConnectionKeepAlive, messages.SettingsConnectionKeepAliveDescription, keepAlive[:], selectedKeepAlive)
			page.row(messages.SettingsConnectionReconnect, messages.SettingsConnectionReconnectDescription, reconnect[:], selectedReconnect)
			page.toggleRow(messages.SettingsShare, messages.SettingsShareDescription, state.shareConnections != shareConnectionsOff, hitSettingsShareToggle)
			page.row(messages.SettingsRestore, messages.SettingsRestoreDescription, restore[:], selectedRestore)
			state.paintOutboundProxyRow(page)
		})
		state.paintSSHSyncSettings(page)
	})
}

// groups declares the groups of a category, a column with room between them.
func (page *settingsPage) groups(children func()) {
	page.f.Column().Gap(settingsGroupGap).Children(children)
}

// group declares a titled card whose rows children declares. The card is a
// rounded, bordered column; the box inside it takes back the border's room, so a
// row spans the whole card and a hairline the card less its border.
func (page *settingsPage) group(title string, rows func()) {
	f, pal := page.f, page.state.pal
	shift := float32(0)
	if page.opened > 0 {
		shift = settingsGroupGap
	}
	page.opened++
	page.rows = 0
	wrapper := f.Column()
	page.matchAnchor(title, wrapper, shift)
	wrapper.Children(func() {
		if title != "" {
			f.Text(title).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(22).MarginSides(0, 4, 6, 4)
		}
		f.Column().Background(pal.surface).Radius(radius.surface).Border(1, withAlpha(pal.line, 0.85)).Children(func() {
			f.Column().Margin(-1).Children(rows)
		})
	})
}

// matchAnchor notes the box when its label is what a search result asked the page
// to scroll to; shift is the room the group gap puts between the box and the
// place the scroll was once measured at.
func (page *settingsPage) matchAnchor(label string, box flex.Box, shift float32) {
	view := &page.state.settings
	if view.jump == "" || label != view.jump {
		return
	}
	page.anchor, page.anchored, page.anchorShift = box, true, shift
	view.jump = ""
}

// separator declares the hairline above a row, unless it opens its card. It takes
// no room: the row starts where the line does.
func (page *settingsPage) separator() {
	if page.rows > 0 {
		page.f.Column().Height(1).MarginSides(0, 1, -1, 1).Background(withAlpha(page.state.pal.line, 0.7))
	}
	page.rows++
}

// rowOf opens a row of a card of the given height.
func (page *settingsPage) rowOf(height float32) flex.Box {
	page.separator()
	return page.f.Row().Height(height).Items(flex.Center)
}

// line is one row of a group: the name and explanation on the left, wrapping to
// the room the control leaves, and a slot of controlWidth on the right, which it
// returns. In a card too narrow for its name and its control, the control keeps
// to the card (the name has no room and is not drawn) instead of hanging out of it.
func (page *settingsPage) line(label, description string, controlWidth float32) flex.Box {
	return page.lineWith(label, description, controlWidth, nil)
}

// lineWith is line whose control slot holds what inside declares, against its
// right edge.
func (page *settingsPage) lineWith(label, description string, controlWidth float32, inside func()) flex.Box {
	return page.lineFor(ui.ID{}, label, description, controlWidth, inside)
}

// lineFor is lineWith whose words are the label of the control with that
// identity, as a Field's label is: pressing them focuses and clicks it, which for
// a switch flips it. A zero identity makes the words plain text.
func (page *settingsPage) lineFor(labelFor ui.ID, label, description string, controlWidth float32, inside func()) flex.Box {
	f, pal := page.f, page.state.pal
	controlWidth = min(controlWidth, max(page.width-60, 0))
	var control flex.Box
	row := page.rowOf(page.rowHeight)
	page.matchAnchor(label, row, 0)
	row.PadX(18).Gap(24).Children(func() {
		words := f.Column().Grow(1).Children(func() {
			f.Text(label).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(pal.text).LineHeight(21)
			f.Paragraph(description, settingsDescLines).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(15).MarginSides(1, 0, 0, 0)
		})
		page.labels(labelFor, words, label, false)
		control = f.Row().Size(controlWidth, 34).Shrink(0).Justify(flex.End).Items(flex.Center)
		if inside != nil {
			control.Children(inside)
		}
	})
	return control
}

// rowFrame is a setting's name and explanation with a slot for its control,
// right-aligned.
func (page *settingsPage) rowFrame(label, description string) flex.Box {
	return page.rowFrameFor(label, description, 0)
}

// rowFrameFor is rowFrame for a control that needs at least minWidth: what the
// words are left with is what the control does not take.
func (page *settingsPage) rowFrameFor(label, description string, minWidth float32) flex.Box {
	return page.line(label, description, max(min(float32(300), max(float32(190), page.width*0.40)), minWidth))
}

func (page *settingsPage) row(label, description string, options []components.SegmentOption, selected ui.ID) {
	need := components.SegmentedControlWidth(page.f.Canvas(), page.state.Theme(), options)
	page.segments(page.rowFrameFor(label, description, need), options, selected)
}

// toggleRow is a setting that is on or off: a switch, which one press flips. The
// id stays the same in both states, so the keyboard's place in the page does not
// move when it is pressed.
func (page *settingsPage) toggleRow(label, description string, on bool, id ui.ID) {
	// The control is only a switch: the rest of the row is for the words.
	page.lineFor(id, label, description, aiSwitchWidth, func() {
		page.switchSlot(id, on, false, label)
	})
}

// actionRow is a settings row whose control is buttons, not a choice.
func (page *settingsPage) actionRow(label, description string, disabled bool, actions ...components.ButtonAction) {
	// Room for the buttons as wide as their labels, and no more: what is left is
	// for the explanation, which wraps to it.
	theme := page.state.Theme()
	controlWidth := theme.Metrics.GapItem * float32(len(actions)-1)
	for _, action := range actions {
		controlWidth += components.BeginButton(page.f.Canvas(), theme, ui.ID{}, ui.Rect{}).Small().SizeToFit(action.Label).BoundsWidth()
	}
	controlWidth = min(controlWidth, page.width*0.6)
	if page.controlWidth > 0 {
		controlWidth = page.controlWidth
	}
	page.buttons(page.line(label, description, controlWidth), disabled, actions...)
}

// labels records that the words in the box are the label of the control, when
// there is one.
func (page *settingsPage) labels(control ui.ID, words flex.Box, label string, disabled bool) {
	if !control.IsZero() {
		page.add(settingsControl{kind: controlLabel, slot: words, id: control, label: label, disabled: disabled})
	}
}

// add records a control to be painted into its slot.
func (page *settingsPage) add(control settingsControl) {
	view := &page.state.settings
	view.controls = append(view.controls, control)
}

func (page *settingsPage) segments(slot flex.Box, options []components.SegmentOption, selected ui.ID) {
	view := &page.state.settings
	view.options = append(view.options, options...)
	page.add(settingsControl{kind: controlSegments, slot: slot, selected: selected, first: len(view.options) - len(options), count: len(options)})
}

func (page *settingsPage) buttons(slot flex.Box, disabled bool, actions ...components.ButtonAction) {
	view := &page.state.settings
	view.actions = append(view.actions, actions...)
	page.add(settingsControl{kind: controlButtons, slot: slot, disabled: disabled, first: len(view.actions) - len(actions), count: len(actions)})
}

func (page *settingsPage) switchIn(slot flex.Box, id ui.ID, on, disabled bool, label string) {
	page.add(settingsControl{kind: controlSwitch, slot: slot, id: id, on: on, disabled: disabled, label: label})
}

// button declares a small button and records it.
func (page *settingsPage) button(id ui.ID, label string, style aiButtonStyle, disabled bool) flex.Box {
	slot := page.state.aiButtonSlot(page.f, label)
	page.add(settingsControl{kind: controlButton, slot: slot, id: id, label: label, style: style, disabled: disabled})
	return slot
}

// switchSlot declares a switch and records it.
func (page *settingsPage) switchSlot(id ui.ID, on, disabled bool, label string) flex.Box {
	slot := aiSwitchSlot(page.f).Shrink(0)
	page.switchIn(slot, id, on, disabled, label)
	return slot
}

// hitOn makes the box a pointer target once the page has been laid out.
func (page *settingsPage) hitOn(box flex.Box, id ui.ID, label string, options ui.HitOptions) {
	page.add(settingsControl{kind: controlHit, slot: box, id: id, label: label, hit: options})
}
