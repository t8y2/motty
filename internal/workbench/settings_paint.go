package workbench

import (
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	// settingsValueMax caps how wide the value a row names is drawn before it is
	// cut: a long font family must not push the row's own name out of the card.
	settingsValueMax = float32(180)
	// settingsChoiceMax is the largest number of options a choice row offers.
	settingsChoiceMax = 4
)

// settingsControlKind is how a recorded control is painted.
type settingsControlKind uint8

const (
	controlHit            settingsControlKind = iota // the slot is a pointer target
	controlWordSeparators                            // the terminal's word-selection separators
	controlSwatches                                  // a strip of round color dots
	controlLabel                                     // the words of a row, which label the control beside them
	controlLogo                                      // the product wordmark in a slot
)

// settingsControl is one control of the page, recorded while the page is
// declared and painted into its slot once the page has been laid out: a flex
// frame paints when it ends, and the keyboard visits controls in the order they
// are painted, so everything interactive is painted here, in the order the page
// declares it. The arrays are copies, so a record holds no pointer into the
// section that made it, and the records live in slices the page reuses.
type settingsControl struct {
	kind     settingsControlKind
	slot     ui.Box
	id       gfx.ID
	label    string
	on       bool
	disabled bool
	style    aiButtonStyle
	hit      gfx.HitOptions
	// first and count name the control's swatches in the page's arena.
	first, count int
}

// settingsPage declares the settings page. The sections call group and the row
// methods inside it; every control they name is recorded in the state and painted
// by paintSettingsControls after the page has ended.
type settingsPage struct {
	state *State
	c     ui.C
	// scroll is the column of groups, whose content height the wheel clamps to.
	scroll ui.Box
	// width is the width of a group's card.
	width float32
	// rowHeight is the height of the rows of the groups being declared.
	rowHeight float32
	// controlWidth, when set, is the width of the buttons of an action row.
	controlWidth float32
	// choiceOptions is scratch room for the labels of one choice row, reused
	// between rows so measuring them allocates nothing.
	choiceOptions [settingsChoiceMax]components.SegmentOption
	rows          int // rows declared in the current card
	opened        int // groups declared so far
	// navList is the scrolling column of categories, whose height and content
	// say how far the list can be scrolled.
	navList ui.Box

	// anchor is what the search result being opened asked to scroll to, once the
	// declaration has met it; anchorShift is the room above it that the scroll
	// leaves.
	anchor      ui.Box
	anchored    bool
	anchorShift float32
}

// The room above the page's title and below its last group, which scroll with
// the page.
const (
	settingsPageTop    = float32(34)
	settingsPageBottom = float32(28)
)

// settingsWorkspacePart names the settings content's container in the workbench
// page. The page declares it at the section workspace's rectangle, so the
// category list, the page's scroll and every control they name are the
// workbench's: its keyboard, pointer, wheel and text routing reach them, and the
// settings need no page of their own.
const settingsWorkspacePart = "settings-workspace"

// settingsWorkspaceBounds is the rectangle the shell gives the section
// workspace, less its border: the settings content fills it. The shell lays that
// slot out with flex; this is the same rectangle, computed before the page has
// been laid out so the settings can be declared in it.
func (state *State) settingsWorkspaceBounds(window gfx.Rect) gfx.Rect {
	chrome := &state.chrome
	left := state.toolbarDockSideReserveFor(true, false)
	return gfx.Rect{
		X:      left + navigationWidth,
		Y:      windowBarHeight,
		Width:  max(window.Width-left-navigationWidth-chrome.shellCardRight, 0),
		Height: max(window.Height-windowBarHeight-state.toolbarDockBottomReserve(false)-sectionCardMargin, 0),
	}.Inset(gfx.All(workspaceBorder))
}

// declareSettingsWorkspace declares the settings content in the workbench page:
// the category list on the left and the scrolling column of groups on the right,
// inside a container the page names by Part at the workspace slot. The page then
// owns the scrolls, the Choices and the controls they name.
func (state *State) declareSettingsWorkspace(c ui.C) {
	view := &state.settings
	bounds := state.settingsWorkspaceBounds(c.Bounds())
	view.pageView = bounds.Height
	maxScroll := max(state.settingsContent-bounds.Height, 0)
	navMax := max(view.navContent-view.navView, 0)
	if clamped := min(max(state.settingsScroll, 0), maxScroll); clamped != state.settingsScroll {
		state.settingsScroll = clamped
		view.pageMotion = gfx.Motion{} // a correction, not a wheel gesture
	}
	if clamped := min(max(view.navScroll, 0), navMax); clamped != view.navScroll {
		view.navScroll = clamped
		view.navMotion = gfx.Motion{}
	}
	// The wheel wrote a target; the page lays out a value on its way there.
	// Animate keeps that value between paints, and the motion the wheel left in
	// the view is what carries it frame after frame: passing it again with the
	// same target changes nothing. A programmatic jump clears it, so the next
	// paint lands at once. Without a frame clock (tests, static previews)
	// Animate returns the target, so those paint the settled position.
	pageShown, navShown := state.settingsScroll, view.navScroll
	if canvas := c.Canvas(); canvas != nil {
		pageShown = canvas.Animate(hitSettingsPageScroll, 0, state.settingsScroll, view.pageMotion)
		navShown = canvas.Animate(hitSettingsNavScroll, 0, view.navScroll, view.navMotion)
	}
	view.pageShown, view.navShown = min(pageShown, maxScroll), min(navShown, navMax)

	pal := state.pal
	navWidth := min(settingsNavWidthMax, max(bounds.Width*0.26, 150))
	areaWidth := max(bounds.Width-navWidth-1, 0)
	title := state.settingsCategoryName(state.settingsCategory)
	searching := state.settingsSearching()
	if searching {
		title = state.messages.Tools.SearchTitle
	}

	view.controls, view.actions, view.colors = view.controls[:0], view.actions[:0], view.colors[:0]
	page := settingsPage{state: state, c: c, width: min(settingsColumnMax, max(areaWidth-64, 0)), rowHeight: settingsRowHeight}
	// The wheel in settings.go owns the targets; each area starts at the value
	// the page shows on the way to one, and the paint clamps the target
	// afterwards.
	c.Part(settingsWorkspacePart)
	ui.Stack(c).Absolute().Left(bounds.X).Top(bounds.Y).Width(bounds.Width).Height(bounds.Height).Clip().Children(func() {
		ui.Row(c).Fill().Children(func() {
			page.nav(navWidth, searching)
			ui.Column(c).Width(1).FillHeight().Background(withAlpha(pal.line, 0.8))
			page.scroll = ui.ScrollAt(c, view.pageShown).Grow(1).Basis(0).Items(ui.Center)
			page.scroll.Children(func() {
				ui.Column(c).FillWidth().MaxWidth(settingsColumnMax).MarginSides(settingsPageTop, 32, settingsPageBottom, 32).Children(func() {
					page.header(title, searching)
					if searching {
						state.paintSettingsResults(&page)
					} else {
						state.paintSettingsCategory(&page)
					}
				})
			})
		})
	})
	view.declared = page
	// The complete color collection is a Drawer of this same page, over the
	// quick picks; it takes the page's pointer, keyboard and wheel while open.
	if state.settingsCategory == settingsTerminal && !searching && state.modal == modalNone && state.schemeBrowserOpen {
		state.declareSchemeDrawer(c)
	}
}

// paintSettingsDetails paints what the settings declared into its slots once the
// workbench page has laid it out: the controls recorded during the declaration,
// the scroll offsets the layout settled, and the scroll a search result asked
// for. The page itself already painted the cards and the scrolling columns.
func (state *State) paintSettingsDetails(canvas *gfx.Canvas) {
	view := &state.settings
	page := &view.declared
	state.settingsContent = page.scroll.Content()
	view.navContent, view.navView = page.navList.Content(), page.navList.Rect().Height
	view.navRect = page.navList.Rect()
	// The layout clamps an area to its content. That correction belongs to the
	// target, not to the animated value the paint laid out: writing the shaken
	// value back would stop the motion part way. A clamp the user did not ask
	// for lands on the next paint.
	if maxScroll := max(state.settingsContent-view.pageView, 0); state.settingsScroll > maxScroll {
		state.settingsScroll = maxScroll
		view.pageMotion = gfx.Motion{}
		canvas.RequestFrame()
	}
	if maxNav := max(view.navContent-view.navView, 0); view.navScroll > maxNav {
		view.navScroll = maxNav
		view.navMotion = gfx.Motion{}
		canvas.RequestFrame()
	}
	state.paintSettingsControls(canvas)

	if page.anchored {
		// Asked to scroll to a setting: it is known where it is now that the page
		// is laid out; one more paint shows the page there. The row sits at the
		// offset the paint showed, so the anchor is measured from that value.
		contentY := page.anchor.Rect().Y - page.scroll.Rect().Y + view.pageShown
		state.settingsScroll = max(contentY-2*settingsPageTop-8-page.anchorShift, 0)
		view.pageMotion = gfx.Motion{}
		view.jumped = true
	}
	if view.jumped {
		view.jumped = false
		canvas.RequestFrame()
	}
}

// paintSettingsControls paints the controls the page recorded into their slots,
// in the order they were declared.
func (state *State) paintSettingsControls(canvas *gfx.Canvas) {
	for index := range state.settings.controls {
		control := &state.settings.controls[index]
		rect := control.slot.Rect()
		switch control.kind {
		case controlHit:
			canvas.HitWithOptions(control.id, rect, control.label, control.hit)
		case controlLabel:
			canvas.HitLabel(control.id, rect, control.label, control.disabled)
		case controlWordSeparators:
			state.paintWordSeparatorsInput(canvas, rect)
		case controlSwatches:
			// The dots are the drawing: a strip of circles a pitch apart.
			for slot, color := range state.settings.colors[control.first : control.first+control.count] {
				canvas.Rounded(gfx.Rect{X: rect.X + float32(slot)*swatchPitch, Y: rect.Y, Width: swatchSize, Height: swatchSize}, swatchSize/2, color)
			}
		case controlLogo:
			state.paintLockup(canvas, rect)
		}
	}
}

// header is the page's title and, for a category with a page-wide action, that
// action against the right edge.
func (page *settingsPage) header(title string, searching bool) {
	c, pal := page.c, page.state.pal
	row := ui.Row(c).FillWidth().Items(ui.Center).Justify(ui.Between).MarginSides(0, 0, 20, 0)
	row.Children(func() {
		ui.Text(c, title).FontSize(26).Bold().Color(pal.text).LineHeight(34)
		if searching {
			return
		}
		switch page.state.settingsCategory {
		case settingsShortcuts:
			messages := page.state.messages
			page.headerButtons(
				components.ButtonAction{ID: hitShortcutClearAll, Label: messages.ShortcutsClearAll, Variant: components.ButtonOutline, Disabled: !page.state.shortcuts.anyBound()},
				components.ButtonAction{ID: hitShortcutReset, Label: messages.ShortcutsReset, Variant: components.ButtonOutline},
			)
		}
	})
}

// headerButtons are the page-wide actions in the title row. They are recorded
// like a row's buttons, so the keyboard reaches them and the page paints them in
// place.
func (page *settingsPage) headerButtons(actions ...components.ButtonAction) {
	slot := ui.Row(page.c).Height(34).Shrink(0).Items(ui.Center).Justify(ui.End)
	page.buttons(slot, false, actions...)
}

// canvas is the page's canvas: a control paints from it and a label is measured
// with it. Reading it from the Frame is not declaring layout.
func (page *settingsPage) canvas() *gfx.Canvas { return page.c.Canvas() }

func (state *State) paintSettingsCategory(page *settingsPage) {
	switch state.settingsCategory {
	case settingsGeneral:
		state.paintGeneralSettings(page)
	case settingsAppearance:
		state.paintAppearanceSettings(page)
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
	page.groups(func() {
		page.group("", func() {
			settingsChoiceRow(page, messages.SettingsLanguage, "", &state.language, settingsLanguageChoices[:], state.settingsLanguageLabel,
				func() { state.languageChosen = true; state.applyLanguage(); state.save() },
				func() {
					if !state.languageChosen {
						state.languageChosen = true
						state.save()
					}
				})
			settingsChoiceRow(page, messages.SettingsTabDoubleClick, messages.SettingsTabDoubleClickDescription, &state.tabDoubleClick, settingsTabDoubleClickChoices[:], state.settingsTabDoubleClickLabel,
				func() { state.save() }, nil)
			page.toggleRow(messages.SettingsTabNumbers, messages.SettingsTabNumbersDescription, state.tabNumbers, hitSettingsTabNumbers)
			numberKey := state.tabNumberChoice()
			settingsChoiceRow(page, messages.SettingsTabNumberKeys, messages.SettingsTabNumberKeysDescription, &numberKey, tabNumberChoices[:], state.tabNumberChoiceLabel,
				func() { state.setTabNumberModifier(numberKey.id()); state.save() }, nil)
			page.toggleRow(messages.SettingsOutputAlerts, messages.SettingsOutputAlertsDescription, state.backgroundOutputAlerts, hitSettingsOutputAlerts)
			if state.options.StartAtLogin != nil {
				state.paintLoginSetting(page)
			}
			if state.options.SetTrayIcon != nil {
				state.paintTrayIconSetting(page)
			}
			if state.closeActionVisible() {
				settingsChoiceRow(page, messages.Tools.GeneralClose, messages.Tools.GeneralCloseDescription, &state.closeAction, settingsCloseChoices[:], state.settingsCloseLabel,
					func() { state.save() }, nil)
			}
			settingsChoiceRow(page, messages.Tools.GeneralKeepWindows, messages.Tools.GeneralKeepWindowsDescription, &state.keepWindows, settingsKeepWindowsChoices[:], state.settingsKeepWindowsLabel,
				func() { state.save() }, nil)
			if state.options.NewWindow != nil {
				settingsChoiceRow(page, messages.Tools.GeneralRelaunch, messages.Tools.GeneralRelaunchDescription, &state.relaunchAction, settingsRelaunchChoices[:], state.settingsRelaunchLabel,
					func() { state.save() }, nil)
			}
			state.paintToolbarSettingsRows(page)
			state.paintSidebarSettingsRow(page)
		})
		state.paintDiagnosticsSetting(page)
	})
}

func (state *State) paintAppearanceSettings(page *settingsPage) {
	messages := state.messages
	page.groups(func() {
		page.group("", func() {
			settingsChoiceRow(page, messages.SettingsAppearance, "", &state.appearanceMode, settingsAppearanceChoices[:], state.settingsAppearanceLabel,
				func() { state.chooseAppearanceMode(state.appearanceMode); state.save() }, nil)
			if state.terminalTransparent() {
				state.paintTerminalTransparencySetting(page)
			}
			settingsChoiceRow(page, messages.SettingsCorners, "", &state.cornerStyle, settingsCornerChoices[:], state.settingsCornerLabel,
				func() { state.applyMetrics(); state.save() }, nil)
			state.paintIconColorSetting(page)
			page.actionRow(messages.SettingsTextSize, messages.SettingsTextSizeDescription, false,
				components.ButtonAction{ID: hitSettingsTextSizeDown, Label: "−", Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitSettingsTextSizeReset, Label: state.interfaceTextSizeLabel(), Variant: components.ButtonGhost},
				components.ButtonAction{ID: hitSettingsTextSizeUp, Label: "+", Variant: components.ButtonOutline})
			if state.interfaceFontRowVisible() {
				page.valueActionRow(messages.SettingsInterfaceFont, state.interfaceFontDescription(), state.interfaceFontInEffect(), !state.uiFont.supported,
					components.ButtonAction{ID: hitInterfaceFontChoose, Label: state.messages.Tools.FontChoose, Variant: components.ButtonOutline})
			}
			settingsChoiceRow(page, messages.SettingsToolbarPosition, messages.SettingsToolbarPositionDescription, &state.terminalToolbarPosition, settingsToolbarPositionChoices[:], state.settingsToolbarPositionLabel,
				func() { state.applyTerminalToolbarPosition(); state.save() }, nil)
			settingsChoiceRow(page, messages.SettingsTabOrientation, messages.SettingsTabOrientationDescription, &state.tabOrientation, settingsTabOrientationChoices[:], state.settingsTabOrientationLabel,
				func() { state.applyTabOrientation(); state.save() }, nil)
			if state.tabOrientation == tabsVertical {
				page.toggleRow(messages.SettingsTabsAutoHide, messages.SettingsTabsAutoHideDescription, state.verticalTabs.autoHide, hitSettingsVerticalTabsAutoHide)
				page.toggleRow(messages.SettingsTabsGrouping, messages.SettingsTabsGroupingDescription, state.verticalTabs.grouping, hitSettingsVerticalTabsGrouping)
			}
		})
	})
}

func (state *State) paintTerminalTransparencySetting(page *settingsPage) {
	c, slider := page.c, &state.terminalTransparency
	label := state.messages.SettingsTransparency
	page.lineFor(hitTerminalTransparency, label, state.messages.SettingsTransparencyDescription, 182, func() {
		ui.Row(c).Grow(1).Gap(8).Items(ui.Center).Children(func() {
			wasDragging := slider.Dragging()
			control := ui.Slider(c, slider).Range(0, 100).Step(1).Label(label).Hit(hitTerminalTransparency, label).Grow(1).Shrink(1)
			// The page routes slider keys by the state ID; match the explicit row hit.
			slider.ID = hitTerminalTransparency
			changed := control.Changed()
			if !slider.Dragging() && (wasDragging || changed) {
				state.save()
			}
			ui.Text(c, strconv.Itoa(int(slider.Value))+"%").FontSize(metrics.FontSizeS).Color(state.pal.muted).Width(36).Shrink(0).TextEnd()
		})
	})
}

func (state *State) paintTerminalSettings(page *settingsPage) {
	messages := state.messages
	page.groups(func() {
		page.group(messages.Completion.Group, func() {
			status, _ := state.completionCatalogStatus()
			page.actionRow(messages.Completion.CatalogRow, status, false,
				components.ButtonAction{ID: hitCompletionUpdate, Label: messages.Completion.Check, Variant: components.ButtonOutline})
			completion := state.completionMode()
			settingsChoiceRow(page, messages.History.SuggestRow, messages.History.SuggestDescription, &completion, settingsCompletionChoices[:], state.settingsCompletionLabel,
				func() { state.setCompletionMode(completion); state.save() }, nil)
			page.toggleRow(messages.Completion.AcceptHintRow, messages.Completion.AcceptHintDescription, !state.history.acceptHintOff, hitSettingsCompletionHint)
			page.toggleRow(messages.History.SettingsRow, messages.History.SettingsDescription, state.history.on, hitSettingsHistoryToggle)
			page.actionRow(messages.History.ClearRow, messages.History.ClearDescription, false,
				components.ButtonAction{ID: hitSettingsHistoryClear, Label: messages.History.Clear, Variant: components.ButtonOutline})
		})
		page.group("", func() {
			settingsChoiceRow(page, messages.SettingsTerminalOption, messages.SettingsTerminalOptionDescription, &state.terminalOption, settingsOptionChoices[:], state.settingsOptionLabel,
				func() { state.save() }, nil)
			page.actionRow(messages.SettingsTerminalFontSize, messages.SettingsTerminalFontSizeDescription, false,
				components.ButtonAction{ID: hitSettingsFontSizeDown, Label: "−", Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitSettingsFontSizeReset, Label: state.fontSizeLabel(), Variant: components.ButtonGhost},
				components.ButtonAction{ID: hitSettingsFontSizeUp, Label: "+", Variant: components.ButtonOutline})
			settingsChoiceRow(page, state.messages.Tools.LineHeightRow, "", &state.terminalLineHeight, settingsLineHeightChoices[:], state.settingsLineHeightLabel,
				func() { state.save() }, nil)
			page.actionRow(messages.SettingsTerminalFontFamily, state.fontDescription(), false,
				components.ButtonAction{ID: hitSettingsFontPick, Label: state.messages.Tools.FontChoose, Variant: components.ButtonOutline})
			page.actionRow(state.messages.Tools.FontFallbackRow, state.fallbackDescription(), false, state.fallbackActions()...)
			settingsChoiceRow(page, state.messages.Tools.PaddingRow, "", &state.terminalPad, settingsPaddingChoices[:], state.settingsPaddingLabel,
				func() { state.applyTerminalPadding(); state.save() }, nil)
			settingsChoiceRow(page, state.messages.Tools.GutterRow, state.messages.Tools.GutterDescription, &state.terminalGutter, settingsGutterChoices[:], state.settingsGutterLabel,
				func() { state.applyTerminalGutter(); state.save() }, nil)
			page.toggleRow(state.messages.Tools.CursorRow, state.messages.Tools.CursorDescription, !state.terminalCursorSteady, hitSettingsCursorToggle)
			settingsChoiceRow(page, state.messages.Tools.CursorShapeRow, state.messages.Tools.CursorShapeDescription, &state.terminalCursorStyle, settingsCursorStyleChoices[:], state.settingsCursorStyleLabel,
				func() { state.applyTerminalCursorStyle(); state.save() }, nil)
			settingsChoiceRow(page, state.messages.Tools.CursorThicknessRow, "", &state.terminalCursorThickness, settingsCursorThicknessChoices[:], state.settingsCursorThicknessLabel,
				func() { state.save() }, nil)
			settingsChoiceRow(page, state.messages.Tools.IntenseRow, state.messages.Tools.IntenseDescription, &state.terminalIntense, settingsIntenseChoices[:], state.settingsIntenseLabel,
				func() { state.rebuildTerminalLook(); state.save() }, nil)
			settingsChoiceRow(page, messages.SettingsTerminalScrollback, messages.SettingsTerminalScrollbackDescription, &state.terminalScrollback, settingsScrollbackChoices[:], state.settingsScrollbackLabel,
				func() { state.applyTerminalScrollback(); state.save() }, nil)
			settingsChoiceRow(page, messages.SettingsTerminalClipboard, messages.SettingsTerminalClipboardDescription, &state.terminalClipboard, settingsClipboardChoices[:], state.settingsClipboardLabel,
				func() { state.save() }, nil)
			page.toggleRow(state.messages.Tools.CopyOnSelectRow, state.messages.Tools.CopyOnSelectDescription, state.terminalCopyOnSelect, hitSettingsCopyOnSelect)
			page.wordSeparatorsRow()
			page.toggleRow(state.messages.Tools.RightClickPasteRow, state.messages.Tools.RightClickPasteDescription, state.terminalRightClickPaste, hitSettingsRightClickPaste)
			page.toggleRow(state.messages.Tools.InputClickRow, state.messages.Tools.InputClickDescription, !state.terminalInputClickOff, hitSettingsInputClick)
			page.toggleRow(state.messages.Tools.LinkPlainClickRow, state.messages.Tools.LinkPlainClickDescription, state.terminalLinkPlainClick, hitSettingsLinkPlainClick)
			page.toggleRow(state.messages.Tools.BellRow, state.messages.Tools.BellDescription, !state.terminalBellOff, hitSettingsBellToggle)
			tabTitle := state.tabTitleMode
			if tabTitle != tabTitleTerminal && tabTitle != tabTitleDirectory {
				tabTitle = tabTitleConnection
			}
			settingsChoiceRow(page, state.messages.Tools.TabTitleRow, state.messages.Tools.TabTitleDescription, &tabTitle, settingsTabTitleChoices[:], state.settingsTabTitleLabel,
				func() { state.tabTitleMode = tabTitle; state.save() },
				func() { state.tabTitleMode = tabTitle; state.save() })
			page.toggleRow(state.messages.Tools.LatencyRow, state.messages.Tools.LatencyDescription, state.terminalShowLatency, hitSettingsLatencyToggle)
			page.toggleRow(state.messages.Tools.PasteImageRow, state.messages.Tools.PasteImageDescription, !state.pasteImageOff, hitSettingsPasteImageToggle)
			page.toggleRow(state.messages.Tools.PasteConfirmRow, state.messages.Tools.PasteConfirmDescription, !state.pasteConfirmOff, hitSettingsPasteConfirmToggle)
			page.toggleRow(state.messages.Tools.CloseConfirmRow, state.messages.Tools.CloseConfirmDescription, !state.closeConfirmOff, hitSettingsCloseConfirmToggle)
			page.toggleRow(state.messages.Tools.CloseWindowConfirmRow, state.messages.Tools.CloseWindowConfirmDescription, !state.closeWindowConfirmOff, hitSettingsCloseWindowConfirmToggle)
			page.toggleRow(state.messages.Tools.PasswordHintRow, state.messages.Tools.PasswordHintDescription, !state.passwordHintOff, hitSettingsPasswordHintToggle)
			page.toggleRow(state.messages.Tools.NotifyRow, state.messages.Tools.NotifyDescription, !state.notifyOff, hitSettingsNotifyToggle)
			state.paintLocalStartSetting(page)
			state.paintLocalShellSetting(page)
		})
		state.paintSchemeSettings(page)
		state.paintHighlightSettings(page)
		state.paintAutofillSettings(page)
		state.paintBackgroundSettings(page)
		state.paintTerminalFindSettingsGroup(page)
	})
}

func (state *State) paintConnectionSettings(page *settingsPage) {
	messages := state.messages
	page.groups(func() {
		page.group("", func() {
			settingsChoiceRow(page, messages.SettingsConnectionTimeout, messages.SettingsConnectionTimeoutDescription, &state.connectionTimeout, settingsTimeoutChoices[:], state.settingsTimeoutLabel,
				func() { state.save() }, nil)
			settingsChoiceRow(page, messages.SettingsConnectionKeepAlive, messages.SettingsConnectionKeepAliveDescription, &state.connectionKeepAlive, settingsKeepAliveChoices[:], state.settingsKeepAliveLabel,
				func() { state.save() }, nil)
			settingsChoiceRow(page, messages.SettingsConnectionReconnect, messages.SettingsConnectionReconnectDescription, &state.connectionReconnect, settingsReconnectChoices[:], state.settingsReconnectLabel,
				func() { state.applyReconnectPolicy(); state.save() }, nil)
			page.toggleRow(messages.SettingsShare, messages.SettingsShareDescription, state.shareConnections != shareConnectionsOff, hitSettingsShareToggle)
			settingsChoiceRow(page, messages.SettingsRestore, messages.SettingsRestoreDescription, &state.restoreConnect, settingsRestoreChoices[:], state.settingsRestoreLabel,
				func() { state.save() }, nil)
			state.paintStartupSettings(page)
			state.paintSSHAgentRow(page)
			settingsChoiceRow(page, messages.SettingsNetworkProxy, messages.SettingsNetworkProxyDescription, &state.networkProxy, settingsNetworkProxyChoices[:], state.settingsNetworkProxyLabel,
				func() { state.save() }, nil)
			state.paintOutboundProxyRow(page)
			state.paintSSHBrowserSetting(page)
		})
		state.paintProxyProfileSettings(page)
		state.paintSSHSyncSettings(page)
		state.paintHostTagSettings(page)
	})
}

// groups declares the groups of a category, a column with room between them.
func (page *settingsPage) groups(children func()) {
	ui.Column(page.c).Gap(settingsGroupGap).Children(children)
}

// group declares a titled card whose rows children declares. The card is a
// rounded, bordered column; the box inside it takes back the border's room, so a
// row spans the whole card and a hairline the card less its border.
func (page *settingsPage) group(title string, rows func()) {
	c, pal := page.c, page.state.pal
	shift := float32(0)
	if page.opened > 0 {
		shift = settingsGroupGap
	}
	page.opened++
	page.rows = 0
	wrapper := ui.Column(c)
	page.matchAnchor(title, wrapper, shift)
	wrapper.Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(22).MarginSides(0, 4, 6, 4)
		}
		ui.Column(c).Background(pal.surface).Radius(radius.surface).Border(1, withAlpha(pal.line, 0.85)).Children(func() {
			ui.Column(c).Margin(-1).Children(rows)
		})
	})
}

// matchAnchor notes the box when its label is what a search result asked the page
// to scroll to; shift is the room the group gap puts between the box and the
// place the scroll was once measured at.
func (page *settingsPage) matchAnchor(label string, box ui.Box, shift float32) {
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
		ui.Column(page.c).Height(1).MarginSides(0, 1, -1, 1).Background(withAlpha(page.state.pal.line, 0.7))
	}
	page.rows++
}

// rowOf opens a row of a card of the given height.
func (page *settingsPage) rowOf(height float32) ui.Box {
	page.separator()
	return ui.Row(page.c).Height(height).Items(ui.Center)
}

// growRow opens a row of a card that is at least as tall as a normal row but
// grows with what it holds: for words the application does not write, such as
// the reason a sync failed. The padding gives a longer text the same room above
// and below it that a normal row's lines have.
func (page *settingsPage) growRow() ui.Box {
	page.separator()
	return ui.Row(page.c).MinHeight(page.rowHeight).Items(ui.Center).PadY(6)
}

// hintRow is words that explain the rows around them, with no control of their
// own. It grows with the text rather than clipping it.
func (page *settingsPage) hintRow(description string) {
	page.growRow().PadX(18).Children(func() {
		ui.Paragraph(page.c, description, 0).FontSize(metrics.FontSizeS).Color(page.state.pal.muted).LineHeight(15)
	})
}

// line is one row of a group: the name and explanation on the left, wrapping to
// the room the control leaves, and a slot of controlWidth on the right, which it
// returns. In a card too narrow for its name and its control, the control keeps
// to the card (the name has no room and is not drawn) instead of hanging out of it.
func (page *settingsPage) line(label, description string, controlWidth float32) ui.Box {
	return page.lineWith(label, description, controlWidth, nil)
}

// lineWith is line whose control slot holds what inside declares, against its
// right edge.
func (page *settingsPage) lineWith(label, description string, controlWidth float32, inside func()) ui.Box {
	return page.lineFor(gfx.ID{}, label, description, controlWidth, inside)
}

// lineWrap is line whose description takes as many lines as its room needs and
// whose row grows to hold them.
func (page *settingsPage) lineWrap(label, description string, controlWidth float32) ui.Box {
	return page.lineWrapFor(gfx.ID{}, label, description, controlWidth, nil)
}

// lineFor is lineWith whose words are the label of the control with that
// identity, as a Field's label is: pressing them focuses and clicks it, which for
// a switch flips it. A zero identity makes the words plain text.
func (page *settingsPage) lineFor(labelFor gfx.ID, label, description string, controlWidth float32, inside func()) ui.Box {
	return page.lineForLines(labelFor, label, description, controlWidth, settingsDescLines, inside)
}

// lineWrapFor is lineFor whose description takes as many lines as its room
// needs, and whose row grows to hold them: for a status whose words the
// application does not write, such as why the last sync failed.
func (page *settingsPage) lineWrapFor(labelFor gfx.ID, label, description string, controlWidth float32, inside func()) ui.Box {
	return page.lineForLines(labelFor, label, description, controlWidth, 0, inside)
}

// lineForLines is lineFor with a limit on the description's lines: a positive
// count ends a longer text with an ellipsis in a row of the card's normal
// height, and zero takes as many lines as the words need, growing the row.
func (page *settingsPage) lineForLines(labelFor gfx.ID, label, description string, controlWidth float32, descLines int, inside func()) ui.Box {
	c, pal := page.c, page.state.pal
	controlWidth = min(controlWidth, max(page.width-60, 0))
	var control ui.Box
	var row ui.Box
	if descLines == 0 {
		row = page.growRow()
	} else {
		row = page.rowOf(page.rowHeight)
	}
	page.matchAnchor(label, row, 0)
	row.PadX(18).Gap(24).Children(func() {
		words := ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, label).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(21)
			ui.Paragraph(c, description, descLines).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(15).MarginSides(1, 0, 0, 0)
		})
		page.labels(labelFor, words, label, false)
		control = ui.Row(c).Height(34).Shrink(0).Justify(ui.End).Items(ui.Center)
		if controlWidth > 0 {
			control.Width(controlWidth)
		} else {
			control.MaxWidth(page.width * 0.6) // the buttons' own width, as far as the words allow
		}
		if inside != nil {
			control.Children(inside)
		}
	})
	return control
}

// rowFrame is a setting's name and explanation with a slot for its control,
// right-aligned.
func (page *settingsPage) rowFrame(label, description string) ui.Box {
	return page.rowFrameFor(label, description, 0)
}

// rowFrameFor is rowFrame for a control that needs at least minWidth: what the
// words are left with is what the control does not take.
func (page *settingsPage) rowFrameFor(label, description string, minWidth float32) ui.Box {
	return page.line(label, description, max(min(float32(300), max(float32(190), page.width*0.40)), minWidth))
}

// settingsChoiceWidth is the width a choice row's equal shares need: each option
// fits its label with the control's padding on both sides, so the widest label
// sets it. It is the segmented control's own width routine, which drew these
// rows before they became page Choices.
func settingsChoiceWidth[T comparable](page *settingsPage, choices []T, name func(T) string) float32 {
	options := page.choiceOptions[:0]
	for _, choice := range choices {
		options = append(options, components.SegmentOption{Label: name(choice)})
	}
	return components.SegmentedControlWidth(page.canvas(), page.state.Theme(), options)
}

// settingsChoiceRow is a row whose control is a page Choice: value is what the
// choice writes, and changed runs after the user picks an option, when value
// already holds it.
func settingsChoiceRow[T comparable](page *settingsPage, label, description string, value *T, choices []T, name func(T) string, changed, same func()) {
	need := settingsChoiceWidth(page, choices, name)
	// A choice row keeps the room a row's control normally gets, even when its
	// own labels need less, and fills it: the options are equal shares of the
	// control, so a narrow control squeezes the widest label out of its share.
	controlWidth := max(min(float32(300), max(float32(190), page.width*0.40)), need)
	controlWidth = min(controlWidth, max(page.width-60, 0))
	page.lineWith(label, description, controlWidth, func() {
		choice := ui.Choice(page.c, value, choices, name).Width(controlWidth)
		if choice.Changed() {
			changed()
			return
		}
		// A click on the option already chosen changes nothing in the value, but
		// is still a choice: the language of a system default becomes a chosen
		// one that way.
		if _, clicked := choice.ChosenIndex(); clicked && same != nil {
			same()
		}
	})
}

// settingsSelectRow is a row whose control is a page Select: value is what the select
// writes, and changed runs once the paint that delivered the choice is over. dress
// may give the select a hint or a placeholder.
func settingsSelectRow[T comparable](page *settingsPage, label, description string, value *T, choices []T, name func(T) string, dress func(ui.Box) ui.Box, changed func()) {
	controlWidth := max(min(float32(300), max(float32(190), page.width*0.40)), 0)
	controlWidth = min(controlWidth, max(page.width-60, 0))
	page.lineWith(label, description, controlWidth, func() {
		selector := ui.Select(page.c, value, choices, name).Label(label).Width(controlWidth)
		if dress != nil {
			selector = dress(selector)
		}
		if selector.Changed() && changed != nil {
			page.c.After(changed)
		}
	})
}

// toggleRow is a setting that is on or off: a switch, which one press flips. The
// id stays the same in both states, so the keyboard's place in the page does not
// move when it is pressed.
func (page *settingsPage) toggleRow(label, description string, on bool, id gfx.ID) {
	// The control is only a switch: the rest of the row is for the words.
	page.lineFor(id, label, description, aiSwitchWidth, func() {
		page.switchSlot(id, on, false, label)
	})
}

// actionRow is a settings row whose control is buttons, not a choice.
func (page *settingsPage) actionRow(label, description string, disabled bool, actions ...components.ButtonAction) {
	page.buttons(page.line(label, description, page.actionWidth()), disabled, actions...)
}

// actionRowWrap is actionRow whose description takes as many lines as its room
// needs: for a row that reports words the application does not write, such as
// why the last sync failed, the whole of them stays readable.
func (page *settingsPage) actionRowWrap(label, description string, disabled bool, actions ...components.ButtonAction) {
	page.buttons(page.lineWrap(label, description, page.actionWidth()), disabled, actions...)
}

// actionWidth is the room the buttons of an action row take: their own, unless the
// page sets one (a search result's row leaves room for its explanation).
func (page *settingsPage) actionWidth() float32 { return page.controlWidth }

// valueActionRow is actionRow for a setting whose current value the row names
// beside the buttons, so the value is read without pressing them: the choice of
// interface font is the one setting whose buttons say nothing about what is in
// effect now. The value takes room from the buttons' side of the row, so the
// setting's own name keeps the same room whatever the value is; a value too long
// for that room ends in an ellipsis.
func (page *settingsPage) valueActionRow(label, description, value string, disabled bool, actions ...components.ButtonAction) {
	theme, canvas := page.state.Theme(), page.canvas()
	gap := theme.Metrics.GapItem
	// The value is set in the buttons' own size, not a step under it: it stands
	// beside them on the same line, and a size down read as something else
	// entirely. Its quieter colour is what makes it secondary.
	valueWidth := min(canvas.TextWidthWeight(value, metrics.FontSizeM, gfx.WeightMedium), settingsValueMax)
	var buttons ui.Box
	page.lineWith(label, description, page.controlWidth, func() {
		ui.Text(page.c, value).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(page.state.pal.muted).
			Ellipsis().MaxWidth(valueWidth).Grow(1).TextEnd().MarginSides(0, gap, 0, 0)
		buttons = ui.Row(page.c).Height(34).Shrink(0).Justify(ui.End).Items(ui.Center)
	})
	page.buttons(buttons, disabled, actions...)
}

// labels records that the words in the box are the label of the control, when
// there is one.
func (page *settingsPage) labels(control gfx.ID, words ui.Box, label string, disabled bool) {
	if !control.IsZero() {
		page.add(settingsControl{kind: controlLabel, slot: words, id: control, label: label, disabled: disabled})
	}
}

// add records a control to be painted into its slot.
func (page *settingsPage) add(control settingsControl) {
	view := &page.state.settings
	view.controls = append(view.controls, control)
}

// buttons declares the actions as small page buttons against the right edge of
// slot, in the order given, so the last one sits at the corner. They keep the
// application's identities: a press is the central click handler's.
func (page *settingsPage) buttons(slot ui.Box, disabled bool, actions ...components.ButtonAction) {
	c := page.c
	slot.Children(func() {
		ui.Row(c).Gap(page.state.Theme().Metrics.GapItem).Items(ui.Center).Children(func() {
			for _, action := range actions {
				button := ui.Button(c, action.Label).Small().Hit(action.ID, action.Label)
				button = button.Variant(action.Variant)
				button.Disabled(disabled || action.Disabled)
			}
		})
	})
}

// switchSlot declares a switch with the application's identity and returns it.
func (page *settingsPage) switchSlot(id gfx.ID, on, disabled bool, label string) ui.Box {
	return ui.Switch(page.c, page.state.pageFlag(on), "").Label(label).Hit(id, label).Disabled(disabled).Shrink(0)
}

// button declares a small button with the application's identity and returns it.
func (page *settingsPage) button(id gfx.ID, label string, style aiButtonStyle, disabled bool) ui.Box {
	button := ui.Button(page.c, label).Small().Hit(id, label)
	switch style {
	case aiButtonPrimary:
	case aiButtonDanger:
		button = button.DestructiveOutline()
	default:
		button = button.Outline()
	}
	return button.Disabled(disabled)
}

// hitOn makes the box a pointer target once the page has been laid out.
func (page *settingsPage) hitOn(box ui.Box, id gfx.ID, label string, options gfx.HitOptions) {
	page.add(settingsControl{kind: controlHit, slot: box, id: id, label: label, hit: options})
}
