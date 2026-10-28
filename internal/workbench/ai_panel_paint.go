package workbench

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
	"motty/internal/i18n"
)

// Metrics of the panel, in logical pixels.
const (
	aiPad          = float32(14) // the margin around everything in the column
	aiGap          = float32(10) // between two messages
	aiAnswerAction = float32(26) // the last answer's copy and usage row
	aiBodySize     = float32(13)
	aiBodyLine     = float32(19)
	aiSmallSize    = float32(12)
	aiSmallLine    = float32(17)
	aiMonoSize     = float32(12)
	aiTitleRow     = infoBarHeight - 1 // with the divider under it, as tall as the terminal's info bar beside it, whose last pixel is its line
	aiButton       = float32(28)
	aiMaxLines     = 4096 // the most lines one message is laid out in
)

// aiRowView is the measured form of one message: its wrapped lines and the height
// they add up to. It is rebuilt only when what it was built from changed, so
// repainting a long conversation measures nothing. Nothing in it says where
// anything goes: the row is declared with flex at the position the sum of the
// heights above it gives.
type aiRowView struct {
	targetLabel string
	// What the layout was built from.
	valid    bool
	built    bool // expanded when built
	kind     aiItemKind
	status   aiToolStatus
	width    float32
	cell     float32
	title    string
	textData *byte
	textLen  int
	resData  *byte
	resLen   int
	argData  *byte
	argLen   int

	expanded bool // chosen by the user: the reasoning or output is open
	actions  bool // the last assistant message reserves room for copy and usage

	top      float32 // below the top of the list, set by every paint
	height   float32
	textRect ui.Rect // the painted Markdown body, for pointer selection
	// lines are the wrapped lines of a user message, a reasoning or a notice; of a
	// tool card the first commandLines are its command, the rest its output.
	lines        []string
	commandLines int
	// An assistant message is Markdown: parsed once, laid out by Viva.
	doc markdown.Document
	md  components.MarkdownView

	detail  string // the command or path a tool card shows
	excerpt string // the first line of a tool's output
}

func (v *aiRowView) current(item *aiItem, width, cell float32, title string) bool {
	return v.valid && v.kind == item.kind && v.status == item.status && v.width == width && v.cell == cell &&
		v.targetLabel == item.targetLabel &&
		v.built == v.expanded && v.title == title &&
		v.textData == unsafe.StringData(item.text) && v.textLen == len(item.text) &&
		v.resData == unsafe.StringData(item.result) && v.resLen == len(item.result) &&
		v.argData == unsafe.StringData(item.call.Arguments) && v.argLen == len(item.call.Arguments)
}

// runeCells is how many terminal cells a rune takes: two for the wide scripts.
func runeCells(r rune) int {
	switch {
	case r < 0x1100:
		return 1
	case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0xA4CF, r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE6F, r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1FAFF, r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

// monoBreak is where to cut s so that at most cols cells remain in the first part.
func monoBreak(s string, cols int) (end int, done bool) {
	used := 0
	for index, r := range s {
		width := runeCells(r)
		if used+width > cols && used > 0 {
			return index, false
		}
		used += width
	}
	return len(s), true
}

// aiMonoWrap lays value out in lines of at most cols cells, appending them to dst.
// It stops after maxLines and reports whether text was left out.
func aiMonoWrap(dst []string, value string, cols, maxLines int) ([]string, bool) {
	count := 0
	for {
		line, rest, more := strings.Cut(value, "\n")
		line = strings.TrimSuffix(line, "\r")
		for start := 0; ; {
			if count == maxLines {
				return dst, true
			}
			end, done := monoBreak(line[start:], cols)
			dst = append(dst, line[start:start+end])
			count++
			start += end
			if done {
				break
			}
		}
		if !more {
			return dst, false
		}
		value = rest
	}
}

// aiWrap appends value, wrapped as body text of the given size, to the lines of
// view and returns how many lines that made.
func (state *State) aiWrap(canvas *ui.Canvas, view *aiRowView, value string, size, width float32) int {
	panel := &state.ai.panel
	panel.wrapped = canvas.WrapLines(panel.wrapped[:0], value, size, ui.WeightRegular, width, aiMaxLines)
	view.lines = append(view.lines, panel.wrapped...)
	return len(panel.wrapped)
}

// aiToolDetail is what a tool card shows under its name: the command, the
// path, or the raw arguments.
func aiToolDetail(item *aiItem) string {
	arguments := item.call.Arguments
	if arguments == "" || arguments == "{}" {
		return ""
	}
	var parsed struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if json.Unmarshal([]byte(arguments), &parsed) == nil {
		switch {
		case parsed.Command != "":
			return parsed.Command
		case parsed.Path != "":
			return parsed.Path
		}
	}
	if len(arguments) > 300 {
		cut := 300
		for cut > 0 && !utf8.RuneStart(arguments[cut]) {
			cut--
		}
		return arguments[:cut] + "…"
	}
	return arguments
}

// aiExcerpt is the first line with text in it, cut to a short length.
func aiExcerpt(result string) string {
	for _, line := range strings.SplitN(result, "\n", 40) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > 56 {
			return string(runes[:56]) + "…"
		}
		return line
	}
	return ""
}

// aiToolTitle is the localized name of a tool.
func aiToolTitle(m i18n.AIPanelMessages, name string) string {
	switch name {
	case tools.NameListSessions:
		return m.ToolListSessions
	case tools.NameTerminalRead:
		return m.ToolTerminalRead
	case tools.NameTerminalExec:
		return m.ToolTerminalExec
	case tools.NameTerminalExecMany:
		return m.ToolTerminalExecMany
	case tools.NameSFTPList:
		return m.ToolSFTPList
	case tools.NameSFTPRead:
		return m.ToolSFTPRead
	case tools.NameSFTPWrite:
		return m.ToolSFTPWrite
	case tools.NameSFTPRemove:
		return m.ToolSFTPRemove
	}
	return name
}

func aiToolIcon(name string) uint8 {
	switch name {
	case tools.NameTerminalExec, tools.NameTerminalExecMany, tools.NameTerminalRead:
		return iconTerminal
	case tools.NameListSessions:
		return iconHosts
	}
	return iconFile
}

// aiStatusText is the words and the color a tool's status is shown with.
func (state *State) aiStatusText(status aiToolStatus) (string, ui.Color) {
	m, pal := state.messages.AIPanel, state.pal
	switch status {
	case aiToolWaiting:
		return m.StatusWaiting, pal.warning
	case aiToolDone:
		return m.StatusDone, pal.success
	case aiToolFailed:
		return m.StatusFailed, pal.danger
	case aiToolDenied:
		return m.StatusDenied, pal.muted
	}
	return m.StatusRunning, pal.muted
}

func (state *State) aiRiskText(level risk.Level) string {
	m := state.messages.AIPanel
	switch level {
	case risk.Medium:
		return m.RiskMedium
	case risk.High:
		return m.RiskHigh
	case risk.Critical:
		return m.RiskCritical
	}
	return m.RiskLow
}

// aiRiskColors are the badge's text color and its fill for a level.
func (state *State) aiRiskColors(level risk.Level) (foreground, fill ui.Color) {
	pal := state.pal
	switch level {
	case risk.Medium:
		return pal.warning, withAlpha(pal.warning, 0.16)
	case risk.High:
		orange := ui.Color{
			Red: (pal.warning.Red + pal.danger.Red) / 2, Green: (pal.warning.Green + pal.danger.Green) / 2,
			Blue: (pal.warning.Blue + pal.danger.Blue) / 2, Alpha: 1,
		}
		return orange, withAlpha(orange, 0.18)
	case risk.Critical:
		return pal.onAccent, pal.danger
	}
	return pal.success, withAlpha(pal.success, 0.16)
}

// The sizes the parts of a row are made of. A row's height is measured from
// these (aiBuild) and declared with the same numbers (declareAIRow), so the two
// agree; flex never has to be asked how tall a row is.
const (
	aiUserPadY     = float32(8)  // inside a user bubble
	aiUserPadX     = float32(10) //
	aiNoticePadY   = float32(8)  // inside a notice
	aiNoticePadX   = float32(12) //
	aiReasonHeader = float32(24) // the line that opens and closes a reasoning row
	aiReasonBody   = float32(10) // what an open reasoning row adds to its lines
	aiBoxPad       = float32(6)  // around the lines of a tool's command or output
	aiToolPadY     = float32(8)  // above and below the parts of a tool card
	aiToolPad      = float32(10) // at the sides of a tool card
	aiToolAfterCmd = float32(6)  // between a tool's command and its output strip
	aiToolAfterOut = float32(4)  // under a tool's output
	aiToolHeader   = float32(22) // the name line and the output strip
	aiMaxDetail    = 6
	aiMaxOutput    = 16
	aiMaxOutBytes  = 8192
)

// aiBuild measures one message for the given width: the lines it wraps to and its
// height.
func (state *State) aiBuild(canvas *ui.Canvas, view *aiRowView, item *aiItem, width, cell, cellHeight float32, title string) {
	if view.kind != item.kind {
		view.expanded = false
	}
	*view = aiRowView{
		targetLabel: item.targetLabel,
		valid:       true, built: view.expanded, kind: item.kind, status: item.status, width: width, cell: cell, title: title,
		textData: unsafe.StringData(item.text), textLen: len(item.text),
		resData: unsafe.StringData(item.result), resLen: len(item.result),
		argData: unsafe.StringData(item.call.Arguments), argLen: len(item.call.Arguments),
		expanded: view.expanded, top: view.top, lines: view.lines[:0], doc: view.doc, md: view.md,
	}
	switch item.kind {
	case aiUser:
		inner := max(width*0.9-2*aiUserPadX, 40)
		count := state.aiWrap(canvas, view, item.text, aiBodySize, inner)
		view.height = 2*aiUserPadY + float32(count)*aiBodyLine
	case aiTargetChange:
		view.detail = fmt.Sprintf(state.messages.AIPanel.TargetSwitchedFmt, item.text)
		view.height = 30
	case aiAssistant:
		view.doc.Parse(item.text)
		view.height = view.md.Layout(canvas, components.MarkdownStyle{
			Size: aiBodySize, LineHeight: aiBodyLine, MonoFace: state.terminalFontFace, CodeInset: 28, Highlight: highlightCode,
		}, &view.doc, width)
	case aiReasoning:
		view.height = aiReasonHeader
		if view.expanded {
			count := state.aiWrap(canvas, view, item.text, aiSmallSize, width-14)
			view.height += aiReasonBody + float32(count)*aiSmallLine
		}
	case aiNotice:
		count := state.aiWrap(canvas, view, item.text, aiSmallSize, width-2*aiNoticePadX)
		view.height = 2*aiNoticePadY + float32(count)*aiSmallLine
	case aiTool:
		state.aiLayoutTool(view, item, width, cell, cellHeight)
	}
}

// aiBlockHeight is the height of a box around count lines of the terminal's font.
func aiBlockHeight(count int, cellHeight, pad float32) float32 {
	return float32(count)*cellHeight + 2*pad
}

// aiLayoutTool measures a tool card: header, the command, and the output.
func (state *State) aiLayoutTool(view *aiRowView, item *aiItem, width, cell, cellHeight float32) {
	view.detail = aiToolDetail(item)
	view.excerpt = aiExcerpt(item.result)
	cols := max(int((width-2*aiToolPad-12)/cell), 8)
	height := 2*aiToolPadY + aiToolHeader
	if view.detail != "" {
		view.lines, _ = aiMonoWrap(view.lines, view.detail, cols, aiMaxDetail)
		view.commandLines = len(view.lines)
		height += aiBlockHeight(view.commandLines, cellHeight, aiBoxPad) + aiToolAfterCmd
	}
	if item.result != "" {
		height += aiToolHeader
		if view.expanded {
			shown := item.result
			if len(shown) > aiMaxOutBytes {
				cut := aiMaxOutBytes
				for cut > 0 && !utf8.RuneStart(shown[cut]) {
					cut--
				}
				shown = shown[:cut]
			}
			first := len(view.lines)
			var more bool
			view.lines, more = aiMonoWrap(view.lines, shown, cols, aiMaxOutput)
			if more || len(shown) < len(item.result) {
				view.lines = append(view.lines, "…")
			}
			height += aiBlockHeight(len(view.lines)-first, cellHeight, aiBoxPad) + aiToolAfterOut
		}
	}
	view.height = height
}

// aiPanelBeginPaint forgets the geometry of the previous paint, so a panel that
// is not painted again stops catching the wheel.
func (state *State) aiPanelBeginPaint() {
	panel := &state.ai.panel
	panel.rect, panel.list, panel.listing = ui.Rect{}, ui.Rect{}, false
	panel.copyCount = 0
	panel.answer = ui.Rect{}
	panel.linkRows, panel.linkBase = 0, 0
}

// paintAIBarButton is the window-bar button that shows and hides the panel. A
// dot on it says a question waits there while the panel is out of sight. The bar
// places the button; the highlight and the dot are declared over its column.
func (state *State) paintAIBarButton(canvas *ui.Canvas, x float32) {
	m, pal := state.messages.AIPanel, state.pal
	visible := state.aiPanelVisible()
	column := ui.Rect{X: x, Width: barButtonSize, Height: windowBarHeight}
	if visible {
		f := state.aiFlex.barButton.Begin(canvas, state.Theme(), column)
		f.Column().Fill().Center().Children(func() {
			f.Column().Size(barButtonSize, barButtonSize).Background(pal.accentSoft).Radius(radius.item)
		})
		f.End()
	}
	state.paintBarButton(canvas, hitAIToggle, x, iconBot, m.ToggleTip)
	if len(state.ai.approvals) > 0 && !visible {
		f := state.aiFlex.barButton.Begin(canvas, state.Theme(), column)
		f.Column().Fill().Center().Children(func() {
			f.Column().Size(barButtonSize, barButtonSize).PadSides(3, 3, 0, 0).Items(flex.End).Children(func() {
				f.Column().Size(7, 7).Background(pal.warning).Radius(3.5)
			})
		})
		f.End()
	}
}

// paintAIPanel draws the assistant over the active terminal workspace: a column
// the panes made room for, or a drawer when the window is too narrow.
func (state *State) paintAIPanel(canvas *ui.Canvas, size ui.Size) {
	if !state.aiPanelVisible() {
		return
	}
	state.ai.panel.window = ui.Rect{Width: size.Width, Height: size.Height}
	pal := state.pal
	if reserve := state.aiSectionReserve(size); reserve > 0 {
		bounds := ui.Rect{
			X: size.Width - reserve + sectionCardMargin, Y: windowBarHeight,
			Width: reserve - 2*sectionCardMargin, Height: max(size.Height-windowBarHeight-sectionCardMargin, 0),
		}
		canvas.RoundedBorder(bounds, radius.surface, 1, pal.windowStroke, pal.workspace)
		// Keep the resize target in the gutter. Only hover/press/focus paints a
		// separator; the resting cards already have their own rounded borders.
		theme := state.Theme()
		theme.Border = ui.Color{}
		window := ui.Rect{Y: bounds.Y, Width: size.Width, Height: bounds.Height}
		dividerX := bounds.X - (sectionCardMargin+1)/2
		components.BeginResizable(canvas, theme, hitAIDivider, window, dividerX/max(window.Width-1, 1)).
			Limits(0, 1).HandleSize(sectionCardMargin).Paint(state.messages.AIPanel.ResizePanel)
		canvas.Clip(bounds)
		state.paintAIBody(canvas, bounds)
		canvas.Unclip()
		state.paintAIModelMenuOverlay(canvas)
		return
	}
	if reserve := state.aiReserve(state.deck.ActiveWorkspace(), size); reserve > 0 {
		// Under the window bar, at the right: a hairline, then the panel.
		var bounds, divider flex.Box
		f := state.aiFlex.shell.Begin(canvas, state.Theme(), ui.Rect{Width: size.Width, Height: size.Height})
		f.Column().Fill().Children(func() {
			f.Column().Height(windowBarHeight).Shrink(0)
			f.Row().Grow(1).Children(func() {
				f.Spacer()
				f.Row().Width(reserve).Children(func() {
					divider = f.Slot().Width(1).Shrink(0)
					bounds = f.Slot().Grow(1).Background(pal.workspace)
				})
			})
		})
		f.End()
		// The hairline is the panel's left edge, and can be dragged to change its width.
		window := ui.Rect{Y: windowBarHeight, Width: size.Width, Height: max(size.Height-windowBarHeight, 0)}
		components.BeginResizable(canvas, state.Theme(), hitAIDivider, window, divider.Rect().X/max(window.Width-1, 1)).
			Limits(0, 1).HandleSize(8).Paint(state.messages.AIPanel.ResizePanel)
		state.paintAIBody(canvas, bounds.Rect())
		state.paintAIModelMenuOverlay(canvas)
		return
	}
	viewport := state.baseWorkspacePixelBounds(size)
	drawer := components.BeginDrawer(canvas, state.Theme(), viewport, components.DrawerOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitAIDrawerDismiss,
		Side: components.DrawerRight, ContentExtent: min(aiPanelWidth, size.Width*0.92),
		InitialFocusID: hitAIInput,
	})
	layout := drawer.Begin("", "")
	state.paintAIBody(canvas, layout.Content)
	drawer.End()
	state.paintAIModelMenuOverlay(canvas)
}

// paintAIModelMenuOverlay paints the open model menu after the panel body. The
// menu is a window-level popup: beside the home card it may open outside the
// card, so the body's clip and layer must not cut it or its dismiss backdrop.
func (state *State) paintAIModelMenuOverlay(canvas *ui.Canvas) {
	if !state.ai.panel.modelMenu {
		return
	}
	if _, hasModel := state.aiModelText(); !hasModel {
		return
	}
	state.paintAIModelMenu(canvas)
}

func (state *State) paintAIBody(canvas *ui.Canvas, bounds ui.Rect) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	m := state.messages.AIPanel
	panel.rect = bounds
	width := bounds.Width - 2*aiPad
	_, hasModel := state.aiModelText()
	setupLabel, setupSummary := m.SetupModel, m.SetupSummary
	showSetup := !hasModel || ai.needLogin != ""
	if ai.needLogin != "" {
		setupLabel, setupSummary = state.messages.AISettings.CLIAuthLogin, state.messages.AISettings.CLIAuthSignedOut
	}
	boxHeight := state.aiPrepareBottom(canvas, width)
	setupWidth := min(max(canvas.TextWidthWeight(setupLabel, aiSmallSize, ui.WeightMedium)+32, 110), width*0.5)

	var body aiBody
	f := state.aiFlex.body.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Children(func() {
		// The title row: the name, a new conversation, the history, and the way out.
		f.Row().Height(aiTitleRow).PadX(aiPad).Gap(4).Items(flex.Center).Children(func() {
			f.Icon(lucideIcons[iconBot]).Color(pal.accent).MarginSides(0, 4, 0, 0)
			f.Text(m.Title).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(22).Grow(1)
			body.newChat = f.Slot().Size(barToolSize, barToolSize)
			body.history = f.Slot().Size(barToolSize, barToolSize)
			body.close = f.Slot().Size(barToolSize, barToolSize)
		})
		// The way to get a model; once there is one, the message box names it.
		if showSetup {
			f.Row().Height(56).MarginSides(0, aiPad, 8, aiPad).PadX(12).Gap(12).Items(flex.Center).Background(pal.surface).Border(1, pal.line).Radius(radius.control).Children(func() {
				f.Paragraph(setupSummary, 3).FontSize(aiSmallSize).Weight(ui.WeightRegular).Color(pal.muted).LineHeight(aiSmallLine).Grow(1)
				body.setup = f.Slot().Size(setupWidth, 30)
			})
		}
		f.Divider().Background(pal.line)
		body.list = f.Slot().Grow(1)
		state.declareAIBottom(f, &body, boxHeight)
	})
	f.End()

	state.paintAIHeader(canvas, &body)
	if showSetup {
		components.BeginButton(canvas, state.Theme(), hitAISetup, body.setup.Rect()).Small().Default(setupLabel)
	}
	list := body.list.Rect()
	panel.list = list
	panel.listing = panel.history
	if row := panel.textSelect.row; row >= 0 && (row >= len(ai.items) || ai.items[row].kind != aiAssistant) {
		panel.clearAITextSelection()
	}
	switch {
	case panel.history:
		state.paintAIHistory(canvas, list)
	case len(ai.items) == 0:
		state.paintAIEmpty(canvas, list)
	default:
		state.paintAIMessages(canvas, list)
	}
	state.paintAIBottom(canvas, &body)
}

// aiModelText is the line naming the model that answers, and whether there is one.
func (state *State) aiModelText() (string, bool) {
	ai, panel := &state.ai, &state.ai.panel
	if ai.settings.defaultModel == "" {
		return "", false
	}
	for index := range ai.providers {
		provider := &ai.providers[index]
		if provider.ID != ai.settings.defaultProvider || !provider.Enabled {
			continue
		}
		if panel.modelName != provider.Name || panel.modelID != ai.settings.defaultModel {
			panel.modelName, panel.modelID = provider.Name, ai.settings.defaultModel
			panel.modelText = provider.Name + " · " + ai.settings.defaultModel
		}
		return panel.modelText, true
	}
	return "", false
}

// paintAIHeader paints the controls of the title row into their slots.
func (state *State) paintAIHeader(canvas *ui.Canvas, body *aiBody) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	m := state.messages.AIPanel
	state.paintIconButton(canvas, hitAIClose, iconClose, body.close.Rect(), m.Close, false, iconButtonQuiet, pal.workspace)
	state.paintIconButton(canvas, hitAIHistory, iconHistory, body.history.Rect(), m.History, false, historyStyle(panel.history), pal.workspace)
	state.paintIconButton(canvas, hitAINew, iconPlus, body.newChat.Rect(), m.NewConversation, ai.run != nil, iconButtonQuiet, pal.workspace)
}

func historyStyle(open bool) iconButtonStyle {
	if open {
		return iconButtonActive
	}
	return iconButtonQuiet
}

// paintAIEmpty is the panel before the first question: a third of the free room
// above the icon, the title, the hint and the examples, and two thirds below.
func (state *State) paintAIEmpty(canvas *ui.Canvas, list ui.Rect) {
	pal := state.pal
	title, hint, texts := state.aiEmptyTexts()
	var examples [aiExamples]flex.Box
	f := state.aiFlex.empty.Begin(canvas, state.Theme(), list)
	f.Column().Fill().PadX(aiPad).Children(func() {
		f.Spacer().MinHeight(8)
		f.Column().Shrink(0).Children(func() {
			f.Icon(lucideIcons[iconBot]).Size(26, 26).Color(pal.accent).Self(flex.Center)
			f.Text(title).FontSize(15).Bold().Color(pal.text).LineHeight(24).TextCenter().MarginSides(14, 0, 0, 0)
			f.Paragraph(hint, 4).FontSize(aiSmallSize).Weight(ui.WeightRegular).Color(pal.muted).LineHeight(aiSmallLine).TextCenter().MarginSides(8, 12, 0, 12)
			f.Column().Gap(6).MarginSides(16, 0, 6, 0).Children(func() {
				for index := range examples {
					examples[index] = f.Slot().Height(36)
				}
			})
		})
		f.Spacer().Grow(2)
	})
	f.End()

	for index, example := range texts {
		bounds := examples[index].Rect()
		if bounds.Y+bounds.Height > list.Y+list.Height {
			break
		}
		id := hitAIExamples.At(index)
		background := pal.surface
		switch {
		case canvas.Pressed() == id:
			background = pal.surfaceHot
		case canvas.Hovered() == id:
			background = pal.surfaceSoft
		}
		r := state.aiFlex.row.Begin(canvas, state.Theme(), bounds)
		r.Row().Fill().PadSides(0, 10, 0, 12).Gap(8).Items(flex.Center).Background(background).Radius(radius.control).
			HitWith(id, example, ui.HitOptions{Role: ui.RoleButton}).Children(func() {
			r.Text(example).FontSize(aiSmallSize).Weight(ui.WeightMedium).Color(pal.text).LineHeight(aiSmallLine).Ellipsis().Grow(1)
			r.Icon(lucideIcons[iconChevronRight]).Size(14, 14).Color(pal.muted).Shrink(0)
		})
		r.End()
	}
}

// aiEmptyTexts are the words of the empty panel: about the terminal when there
// is one to look at, and about getting one when there is not.
func (state *State) aiEmptyTexts() (title, hint string, examples [aiExamples]string) {
	m := state.messages.AIPanel
	if _, err := state.aiTargetSession(); err != nil {
		return m.IdleTitle, m.IdleHint, [aiExamples]string{m.IdleExample1, m.IdleExample2, m.IdleExample3}
	}
	return m.EmptyTitle, m.EmptyHint, [aiExamples]string{m.Example1, m.Example2, m.Example3}
}

// The history list: a title above rows of aiHistoryRow, two pixels of which are the
// gap between neighbours.
const (
	aiHistoryHeader = float32(34)
	aiHistoryRow    = float32(46)
)

// paintAIHistory lists the saved conversations in place of the messages. Only the
// rows on screen are declared; the rows above and below are one block each.
func (state *State) paintAIHistory(canvas *ui.Canvas, list ui.Rect) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	m := state.messages.AIPanel
	var body flex.Box
	f := state.aiFlex.history.Begin(canvas, state.Theme(), list)
	f.Column().Fill().Children(func() {
		f.Column().Height(aiHistoryHeader).Shrink(0).PadSides(8, aiPad, 0, aiPad).Children(func() {
			f.Text(m.HistoryTitle).FontSize(metrics.FontSizeM).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(22)
		})
		if len(ai.conversations) == 0 {
			f.Column().Grow(1).PadSides(24, aiPad, 0, aiPad).Children(func() {
				f.Text(m.HistoryEmpty).FontSize(aiSmallSize).Color(pal.muted).LineHeight(22).TextCenter()
			})
		} else {
			body = f.Slot().Grow(1)
		}
	})
	f.End()
	if len(ai.conversations) == 0 {
		panel.maxTop = 0
		return
	}

	area := body.Rect()
	count := len(ai.conversations)
	content := float32(count) * aiHistoryRow
	panel.maxTop = max(content-area.Height, 0)
	panel.historyTop = min(panel.historyTop, panel.maxTop)
	first := int(panel.historyTop / aiHistoryRow)
	last := min(int((panel.historyTop+area.Height)/aiHistoryRow), count-1)

	// The rows are placed by a Scroll of slots, then declared one by one into
	// them, so a row's hit is followed by its delete button, in the order the
	// keyboard visits them.
	panel.rowSlots = panel.rowSlots[:0]
	f = state.aiFlex.historyRows.Begin(canvas, state.Theme(), area)
	f.Scroll(panel.historyTop).Fill().PadX(6).Children(func() {
		f.Column().Height(float32(first) * aiHistoryRow)
		for index := first; index <= last; index++ {
			panel.rowSlots = append(panel.rowSlots, f.Slot().Height(aiHistoryRow-2).MarginSides(0, 0, 2, 0))
		}
		f.Column().Height(float32(count-1-last) * aiHistoryRow)
	})
	f.End()

	scroller := components.BeginMessageScroller(canvas, state.Theme(), area, content, panel.historyTop)
	scroller.BeginContent()
	for offset, slot := range panel.rowSlots {
		index := first + offset
		conversation := &ai.conversations[index]
		openID, deleteID := hitAIHistoryOpen.At(index), hitAIHistoryDelete.At(index)
		renameID := hitAIHistoryRename.At(index)
		var background ui.Color
		switch {
		case canvas.Pressed() == openID:
			background = pal.surfaceHot
		case canvas.Hovered() == openID || canvas.Hovered() == deleteID || canvas.Hovered() == renameID:
			background = pal.surfaceSoft
		case conversation.ID == ai.conv:
			background = pal.accentSoft
		}
		var remove, rename flex.Box
		r := state.aiFlex.row.Begin(canvas, state.Theme(), slot.Rect())
		r.Row().Fill().PadSides(0, 4, 0, 10).Gap(10).Items(flex.Center).Background(background).Radius(radius.control).
			HitWith(openID, conversation.Title, ui.HitOptions{Role: ui.RoleButton, Hint: m.HistoryOpen}).Children(func() {
			r.Column().Grow(1).Children(func() {
				r.Text(conversation.Title).FontSize(metrics.FontSizeM).Weight(ui.WeightMedium).Color(pal.text).LineHeight(20).Ellipsis()
				if index < len(panel.historyDates) {
					r.Text(panel.historyDates[index]).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(16)
				}
			})
			rename = r.Slot().Size(aiButton, aiButton)
			remove = r.Slot().Size(aiButton, aiButton)
		})
		r.End()
		state.paintIconButton(canvas, renameID, iconPencil, rename.Rect(), m.HistoryRename, false, iconButtonQuiet, pal.workspace)
		state.paintIconButton(canvas, deleteID, iconTrash, remove.Rect(), m.HistoryDelete, false, iconButtonQuiet, pal.workspace)
	}
	scroller.EndContent()
}

// intersectRect is the part of a that lies inside b.
func intersectRect(a, b ui.Rect) ui.Rect {
	left, top := max(a.X, b.X), max(a.Y, b.Y)
	right, bottom := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if right <= left || bottom <= top {
		return ui.Rect{}
	}
	return ui.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

// paintAIMessages draws the conversation in a scrolling list. Every message keeps
// its measured height (aiRowView), so where each one starts is a running sum and a
// long conversation costs the rows on screen only: those are declared in a flex
// Scroll between two blocks as tall as the rows above and below them, and flex
// places them.
func (state *State) paintAIMessages(canvas *ui.Canvas, list ui.Rect) {
	ai, panel := &state.ai, &state.ai.panel
	m := state.messages.AIPanel
	rowW := list.Width - 2*aiPad
	cell, cellHeight := canvas.MonoCellSize(aiMonoSize, state.terminalFontFace)

	for len(panel.rows) < len(ai.items) {
		panel.rows = append(panel.rows, aiRowView{})
	}
	panel.rows = panel.rows[:len(ai.items)]
	y := aiGap
	for index := range ai.items {
		item, view := &ai.items[index], &panel.rows[index]
		title := ""
		switch item.kind {
		case aiReasoning:
			title = m.Thinking
			if state.aiThinking(index) {
				title = m.ThinkingNow
			}
		case aiTool:
			title = aiToolTitle(m, item.call.Name)
		}
		lastAnswer := index == len(state.ai.items)-1 && item.kind == aiAssistant
		if !view.current(item, rowW, cell, title) || view.actions != lastAnswer {
			// A selection indexes the rendered text, so it survives a rebuild only
			// when the new rendering still begins with the old one. Comparing the
			// Markdown sources instead would keep it across a structure change (an
			// unclosed backtick closing, say) and point it at other characters.
			// The old text is read only while this row holds the selection.
			keepSelection := panel.textSelect.row == index
			var rendered string
			if keepSelection {
				rendered = view.md.SelectedText(components.MarkdownSelection{Focus: view.md.TextLen()})
			}
			state.aiBuild(canvas, view, item, rowW, cell, cellHeight, title)
			view.actions = lastAnswer
			if lastAnswer {
				view.height += aiAnswerAction
			}
			if keepSelection && (item.kind != aiAssistant ||
				!strings.HasPrefix(view.md.SelectedText(components.MarkdownSelection{Focus: view.md.TextLen()}), rendered)) {
				panel.clearAITextSelection()
			}
		}
		if panel.textSelect.row == index && panel.textSelect.sel.Anchor != panel.textSelect.sel.Focus {
			// Clamp each end without ordering them: Anchor must stay where the
			// drag began, or dragging upward would move it on every repaint.
			length := view.md.TextLen()
			anchor := min(max(panel.textSelect.sel.Anchor, 0), length)
			focus := min(max(panel.textSelect.sel.Focus, 0), length)
			if anchor == focus {
				panel.clearAITextSelection()
			} else {
				panel.textSelect.sel = components.MarkdownSelection{Anchor: anchor, Focus: focus}
			}
		}
		view.top = y
		y += view.height + aiGap
	}
	content := y
	panel.maxScroll = max(content-list.Height, 0)
	scroll := ai.scroll.Resolve(list.Height, content)

	// The rows that touch the viewport.
	first, last := -1, -1
	for index := range panel.rows {
		view := &panel.rows[index]
		if view.top+view.height < scroll || view.top > scroll+list.Height {
			continue
		}
		if first < 0 {
			first = index
		}
		last = index
	}

	// The rows are placed by a Scroll of slots, then declared one by one into them
	// under the clip of the viewport, so that a row's hits are followed by its
	// copy buttons, in the order the keyboard visits them.
	panel.rowSlots = panel.rowSlots[:0]
	f := state.aiFlex.messages.Begin(canvas, state.Theme(), list)
	f.Scroll(scroll).Fill().PadX(aiPad).Children(func() {
		if first < 0 {
			return
		}
		f.Column().Height(panel.rows[first].top)
		for index := first; index <= last; index++ {
			panel.rowSlots = append(panel.rowSlots, f.Slot().Height(panel.rows[index].height).MarginSides(0, 0, aiGap, 0))
		}
		end := &panel.rows[last]
		f.Column().Height(content - (end.top + end.height + aiGap))
	})
	f.End()

	scroller := components.BeginMessageScroller(canvas, state.Theme(), list, content, scroll).Buttons(ui.ID{}, "", hitAIToEnd, m.ScrollToEnd)
	scroller.BeginContent()
	for offset, slot := range panel.rowSlots {
		index := first + offset
		view, item := &panel.rows[index], &ai.items[index]
		panel.copies = panel.copies[:0]
		r := state.aiFlex.row.Begin(canvas, state.Theme(), slot.Rect())
		body := state.declareAIRow(r, index, view, item)
		r.End()
		switch {
		case item.kind == aiAssistant:
			state.paintAIAssistant(canvas, body.Rect(), list, view, index)
		case item.kind == aiReasoning && state.aiThinking(index), item.kind == aiTool && item.status == aiToolRunning:
			// Still thinking, or the command is running: a spinner in the slot the row left for it.
			components.PaintSpinner(canvas, state.Theme(), body.Rect(), components.SpinnerPhase(canvas.Now()/spinnerStep))
			canvas.RequestFrameIn(spinnerStep)
		}
	}
	scroller.EndContent()
}

// aiThinking reports whether row index is a thought the model is still writing:
// the last row of a conversation that has a request running.
func (state *State) aiThinking(index int) bool {
	ai := &state.ai
	return ai.run != nil && index == len(ai.items)-1 && ai.items[index].kind == aiReasoning
}

// aiTextLines declares wrapped lines of body text, one element each.
func aiTextLines(f *flex.Frame, lines []string, size, lineHeight float32, color ui.Color) {
	for _, line := range lines {
		f.Text(line).FontSize(size).Weight(ui.WeightRegular).Color(color).LineHeight(lineHeight)
	}
}

// aiMonoLines declares lines in the terminal's font.
func (state *State) aiMonoLines(f *flex.Frame, lines []string, color ui.Color) {
	for _, line := range lines {
		f.Text(line).FontSize(aiMonoSize).MonoFace(state.terminalFontFace).Color(color)
	}
}

// declareAIRow declares one message across the room the list gave it. Its height
// is what its parts add up to, which is what aiBuild measured: the list placed the
// row by that measure, so a row that came out otherwise would overlap the next.
// An assistant message is painted by its Markdown view; the box returned is the
// room that view is painted in, and the copy buttons of its code blocks are
// declared over it.
func (state *State) declareAIRow(f *flex.Frame, index int, view *aiRowView, item *aiItem) flex.Box {
	pal, m := state.pal, state.messages.AIPanel
	row := f.Column()
	var body flex.Box
	switch item.kind {
	case aiUser:
		row.Items(flex.End).Children(func() {
			f.Column().MaxWidth(view.width).PadSides(aiUserPadY, aiUserPadX, aiUserPadY, aiUserPadX).Background(pal.accentSoft).Radius(radius.popup).Children(func() {
				aiTextLines(f, view.lines, aiBodySize, aiBodyLine, pal.text)
			})
		})
	case aiTargetChange:
		row.Height(view.height).Items(flex.Center).Children(func() {
			f.Row().Fill().Gap(10).Items(flex.Center).Children(func() {
				f.Column().Grow(1).Height(1).Background(pal.line)
				f.Text(view.detail).FontSize(aiSmallSize).Color(pal.faint).LineHeight(aiSmallLine).MaxWidth(max(view.width-48, 0)).Ellipsis()
				f.Column().Grow(1).Height(1).Background(pal.line)
			})
		})
	case aiAssistant:
		row.Height(view.height).Children(func() {
			f.Stack().Fill().Children(func() {
				body = f.Slot().Fill()
				// Each code block's copy button sits in its top right corner; where the
				// blocks are is the Markdown view's, once it has laid the message out.
				for _, block := range view.md.Codes() {
					area := block.Bounds
					f.Row().Size(area.Width, area.Height).MarginSides(area.Y, 0, 0, area.X).PadSides(3, 4, 0, 0).Children(func() {
						f.Spacer()
						panel := &state.ai.panel
						panel.copies = append(panel.copies, aiCopySlot{slot: f.Slot().Size(22, 22), code: block.Text})
					})
				}
			})
		})
	case aiReasoning:
		row.Children(func() {
			header := f.Row().Height(aiReasonHeader).PadX(4).Gap(6).Items(flex.Center).Radius(radius.item).Children(func() {
				icon := iconChevronRight
				if view.expanded {
					icon = iconChevronDown
				}
				if state.aiThinking(index) {
					body = f.Slot().Size(12, 12)
				} else {
					f.Icon(lucideIcons[icon]).Size(12, 12).Color(pal.muted)
				}
				f.Text(view.title).FontSize(aiSmallSize).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(18)
			})
			if index < aiMaxRowHits {
				header.HoverBackground(pal.surfaceSoft).HitWith(hitAIRows.At(index), view.title, ui.HitOptions{Role: ui.RoleButton, Expanded: view.expanded})
			}
			if view.expanded {
				// The thread down the left side, and the thought beside it.
				f.Row().MarginSides(2, 0, 0, 0).Children(func() {
					f.Column().Width(1).MarginSides(0, 0, 4, 7).Background(pal.line)
					f.Column().Grow(1).MarginSides(2, 0, 6, 6).Children(func() {
						aiTextLines(f, view.lines, aiSmallSize, aiSmallLine, pal.muted)
					})
				})
			}
		})
	case aiNotice:
		row.PadSides(aiNoticePadY, aiNoticePadX, aiNoticePadY, aiNoticePadX).Background(withAlpha(pal.danger, 0.12)).Radius(radius.control).Children(func() {
			aiTextLines(f, view.lines, aiSmallSize, aiSmallLine, pal.danger)
		})
	case aiTool:
		border := pal.line
		if item.status == aiToolWaiting {
			border = pal.warning
		}
		statusText, statusColor := state.aiStatusText(item.status)
		row.Border(1, border).PadSides(aiToolPadY-1, aiToolPad-1, aiToolPadY-1, aiToolPad-1).Background(pal.surface).Radius(radius.control).Children(func() {
			// The name and the risk at the left, the status at the right.
			f.Row().Height(aiToolHeader).Items(flex.Center).Children(func() {
				f.Icon(lucideIcons[aiToolIcon(item.call.Name)]).Size(14, 14).Color(pal.muted).MarginSides(0, 8, 0, 0)
				f.Text(view.title).FontSize(aiSmallSize).Weight(ui.WeightSemibold).Color(pal.text).LineHeight(20).Ellipsis()
				if item.call.Name == tools.NameTerminalExec || item.call.Name == tools.NameTerminalExecMany {
					foreground, fill := state.aiRiskColors(item.risk)
					f.Row().Height(16).PadX(6).Center().Shrink(0).MarginSides(0, 0, 0, 8).Background(fill).Radius(radius.control).Children(func() {
						f.Text(state.aiRiskText(item.risk)).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(foreground).LineHeight(14)
					})
				}
				f.Spacer().MinWidth(8)
				f.Row().Shrink(0).Items(flex.Center).Children(func() {
					switch item.status {
					case aiToolDone:
						f.Icon(lucideIcons[iconCircleCheck]).Size(13, 13).Color(statusColor).MarginSides(0, 4, 0, 0)
					case aiToolFailed, aiToolDenied:
						f.Icon(lucideIcons[iconAlert]).Size(13, 13).Color(statusColor).MarginSides(0, 4, 0, 0)
					default:
						if item.status == aiToolRunning {
							// Painted as a spinner once the row is placed.
							body = f.Slot().Size(13, 13).MarginSides(0, 5, 0, 0)
						} else {
							f.Column().Size(7, 7).Background(statusColor).Radius(3.5).MarginSides(0, 5, 0, 0)
						}
					}
					f.Text(statusText).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(statusColor).LineHeight(aiToolHeader)
				})
			})
			if view.commandLines > 0 {
				f.Column().Pad(aiBoxPad).MarginSides(0, 0, aiToolAfterCmd, 0).Background(pal.terminal).Radius(radius.item).Children(func() {
					state.aiMonoLines(f, view.lines[:view.commandLines], pal.text)
				})
			}
			if item.result == "" {
				return
			}
			// One line of the output, or all that is kept of it.
			label, icon := m.ShowOutput, iconChevronRight
			if view.expanded {
				label, icon = m.HideOutput, iconChevronDown
			}
			strip := f.Row().Height(aiToolHeader).MarginSides(0, -4, 0, -4).PadX(4).Gap(6).Items(flex.Center).Radius(radius.item).Children(func() {
				f.Icon(lucideIcons[icon]).Size(12, 12).Color(pal.muted)
				f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(16).Shrink(0).MarginSides(0, 4, 0, 0)
				if !view.expanded && view.excerpt != "" {
					f.Text(view.excerpt).FontSize(metrics.FontSizeS).MonoFace(state.terminalFontFace).Color(pal.faint)
				}
			})
			if index < aiMaxRowHits {
				strip.HoverBackground(pal.surfaceSoft).HitWith(hitAIRows.At(index), view.title, ui.HitOptions{
					Role: ui.RoleButton, Expanded: view.expanded, Value: statusText, Hint: label,
				})
			}
			if view.expanded {
				f.Column().Pad(aiBoxPad).MarginSides(0, 0, aiToolAfterOut, 0).Background(pal.terminal).Radius(radius.item).Children(func() {
					state.aiMonoLines(f, view.lines[view.commandLines:], pal.muted)
				})
			}
		})
	}
	if item.kind == aiAssistant || item.kind == aiReasoning && state.aiThinking(index) || item.kind == aiTool && item.status == aiToolRunning {
		return body
	}
	return row
}

// paintAIAssistant paints an assistant message, which is Markdown, in the room
// flex gave it, then the copy buttons of its code blocks over it.
func (state *State) paintAIAssistant(canvas *ui.Canvas, bounds, list ui.Rect, view *aiRowView, index int) {
	pal, m := state.pal, state.messages.AIPanel
	panel := &state.ai.panel
	look := components.MarkdownLookFor(state.Theme())
	blue := pal.ansi[4] // the accent is nearly white; a link has to stand out from the text
	look.Text, look.Muted, look.Link = pal.text, pal.muted, ui.RGB(blue[0], blue[1], blue[2])
	look.CodeFill, look.InlineCodeFill, look.Line, look.Radius = pal.terminal, pal.surfaceSoft, pal.line, radius.control
	// MarkdownLookFor tints a selection with theme.Primary at 50%, and this
	// palette's accent is its text colour: on the dark palette that is a grey
	// band the selected text barely stands out from. Use the terminal's
	// selection recipe instead, so both places look the same.
	look.Selection = withAlpha(pal.text, 0.25)
	ansi := func(index int) ui.Color { return ui.RGB(pal.ansi[index][0], pal.ansi[index][1], pal.ansi[index][2]) }
	look.Code = [components.MarkdownCodeClasses]ui.Color{
		components.CodeComment: pal.faint, components.CodeString: ansi(2), components.CodeNumber: ansi(3),
		components.CodeKeyword: ansi(5), components.CodeFunction: ansi(4), components.CodeVariable: ansi(6),
		components.CodeFlag: ansi(6), components.CodeKey: ansi(4), components.CodeAdded: ansi(2), components.CodeRemoved: ansi(1),
	}
	if panel.linkRows < len(panel.linkTable) {
		panel.linkTable[panel.linkRows] = aiLinkRow{base: panel.linkBase, row: index}
		panel.linkRows++
	}
	text := ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: min(bounds.Height, view.md.Height())}
	view.textRect = text
	if index < aiMaxRowHits {
		canvas.HitWithOptions(hitAIAnswerText.At(index), text, m.AnswerText, ui.HitOptions{Role: ui.RoleGroup, SkipTab: true})
	}
	view.md.Paint(canvas, components.MarkdownPaint{
		Look: look, X: bounds.X, Y: bounds.Y, Clip: list, Links: hitAILinks, Base: panel.linkBase,
		Selection: panel.selectionFor(index),
	})
	panel.linkBase += view.md.LinkCount()
	for _, copy := range panel.copies {
		button := copy.slot.Rect()
		if visible := intersectRect(button, list); visible.Height < button.Height {
			continue
		}
		if panel.copyCount >= aiMaxCopyBlocks {
			break
		}
		id := hitAICopies.At(panel.copyCount)
		panel.copyTargets[panel.copyCount] = copy.code
		panel.copyCount++
		icon := iconCopy
		if panel.copied == copy.code {
			// The check says it was copied for a moment, then the button is itself again.
			if left := aiCopiedFor - (state.now() - panel.copiedAt); left > 0 {
				icon = iconCheck
				canvas.RequestFrameIn(left)
			}
		}
		state.paintIconButton(canvas, id, icon, button, m.CopyCode, false, iconButtonQuiet, pal.terminal)
	}
	if view.actions {
		state.paintAIAnswerActions(canvas, bounds, list, index)
	}
}

// paintAIAnswerActions draws the hover-only copy and token row under the last
// answer. The row's height was reserved when the row was measured.
func (state *State) paintAIAnswerActions(canvas *ui.Canvas, bounds, list ui.Rect, index int) {
	panel, pal := &state.ai.panel, state.pal
	m := state.messages.AIPanel
	panel.answer = bounds
	panel.answerOver = pointInRect(panel.pointer.X, panel.pointer.Y, bounds)
	action := ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - aiAnswerAction, Width: bounds.Width, Height: aiAnswerAction}
	if visible := intersectRect(action, list); visible.Height < action.Height {
		return
	}
	if !pointInRect(panel.pointer.X, panel.pointer.Y, bounds) {
		return
	}
	item := &state.ai.items[index]
	button := ui.Rect{X: action.X, Y: action.Y + 3, Width: 20, Height: 20}
	icon := iconCopy
	if panel.copied == item.text {
		if left := aiCopiedFor - (state.now() - panel.copiedAt); left > 0 {
			icon = iconCheck
			canvas.RequestFrameIn(left)
		}
	}
	state.paintIconButton(canvas, hitAICopyAnswer, icon, button, m.CopyAnswer, false, iconButtonQuiet, pal.workspace)
	if state.ai.lastUsage.InputTokens > 0 || state.ai.lastUsage.OutputTokens > 0 {
		usage := state.aiUsageText()
		canvas.Label(usage, ui.Rect{X: button.X + 26, Y: button.Y + 1, Width: max(action.Width-26, 0), Height: 18},
			ui.TextStyle{Size: aiSmallSize, Color: pal.faint})
	}
}

// aiCopySlot is where the copy button of a code block goes, once flex has placed it.
type aiCopySlot struct {
	slot flex.Box
	code string
}

// aiFlexViews are the layouts the assistant's panel is declared with.
type aiFlexViews struct {
	body, empty, mention, summary, send, menu flex.View
	shell, barButton                          flex.View
	history, historyRows, messages            flex.View
	row                                       flex.View // one row of the history or of the conversation, declared into its slot
}
