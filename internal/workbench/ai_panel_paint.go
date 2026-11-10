package workbench

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
	"unsafe"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
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

// aiPanelPart names the assistant's container in the workbench page. The page
// declares one Part-named, window-filling box for the panel, and the panel's
// body, menus and text live under it: their identities are the page's, so the
// page routes its controls, menus and wheel, while the identities this package
// names by hand (aiPanelHits) keep their own place.
const aiPanelPart = "ai-panel"

// aiPanelScope is where the workbench page names the assistant's elements.
func aiPanelScope() gfx.Scope {
	return tree.Sub(tree.Feature(shellPageRoot), tree.Name(aiPanelPart))
}

// aiPanelPageScratch is what the panel's declaration measures before declaring
// its body, so the body's view reads the values instead of measuring them again.
type aiPanelPageScratch struct {
	boxHeight, setupWidth    float32
	showSetup                bool
	setupLabel, setupSummary string
}

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
	attData  *aiAttachmentRef
	attLen   int

	expanded bool // chosen by the user: the reasoning or output is open
	actions  bool // the last assistant message reserves room for copy and usage

	top      float32 // below the top of the list, set by every paint
	height   float32
	textRect gfx.Rect // the painted Markdown body, for pointer selection
	// lines are the wrapped lines of a user message, a reasoning or a notice; of a
	// tool card the first commandLines are its command, the rest its output.
	lines        []string
	commandLines int
	// An assistant message is Markdown: parsed once, laid out by Viva.
	doc markdown.Document
	md  components.MarkdownView
	// User messages and tool cards use plain-text source maps for the same
	// selection contract. Their slots are transient geometry of the last paint.
	plain       [aiTextPartCount]aiPlainTextView
	textSlots   [aiTextPartCount]ui.Box
	textSlotSet [aiTextPartCount]bool

	detail  string // the command or path a tool card shows
	excerpt string // the first line of a tool's output
}

func (v *aiRowView) current(item *aiItem, width, cell float32, title string) bool {
	return v.valid && v.kind == item.kind && v.status == item.status && v.width == width && v.cell == cell &&
		v.targetLabel == item.targetLabel &&
		v.built == v.expanded && v.title == title &&
		v.textData == unsafe.StringData(item.text) && v.textLen == len(item.text) &&
		v.resData == unsafe.StringData(item.result) && v.resLen == len(item.result) &&
		v.argData == unsafe.StringData(item.call.Arguments) && v.argLen == len(item.call.Arguments) &&
		v.attData == unsafe.SliceData(item.attachments) && v.attLen == len(item.attachments)
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
func (state *State) aiWrap(canvas *gfx.Canvas, view *aiRowView, value string, size, width float32) int {
	panel := &state.ai.panel
	panel.wrapped = canvas.WrapLines(panel.wrapped[:0], value, size, gfx.WeightRegular, width, aiMaxLines)
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
	case tools.NameUploadSelected:
		return m.Upload
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
func (state *State) aiStatusText(status aiToolStatus) (string, gfx.Color) {
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
func (state *State) aiRiskColors(level risk.Level) (foreground, fill gfx.Color) {
	pal := state.pal
	switch level {
	case risk.Medium:
		return pal.warning, withAlpha(pal.warning, 0.16)
	case risk.High:
		orange := gfx.Color{
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
func (state *State) aiBuild(canvas *gfx.Canvas, view *aiRowView, item *aiItem, width, cell, cellHeight float32, title string) {
	if view.kind != item.kind {
		view.expanded = false
	}
	*view = aiRowView{
		targetLabel: item.targetLabel,
		valid:       true, built: view.expanded, kind: item.kind, status: item.status, width: width, cell: cell, title: title,
		textData: unsafe.StringData(item.text), textLen: len(item.text),
		resData: unsafe.StringData(item.result), resLen: len(item.result),
		argData: unsafe.StringData(item.call.Arguments), argLen: len(item.call.Arguments),
		attData: unsafe.SliceData(item.attachments), attLen: len(item.attachments),
		expanded: view.expanded, top: view.top, lines: view.lines[:0], doc: view.doc, md: view.md, plain: view.plain,
	}
	switch item.kind {
	case aiUser:
		inner := max(width*0.9-2*aiUserPadX, 40)
		view.plain[aiTextPrimary].layoutBody(canvas, item.text, aiBodySize, aiBodyLine, inner, aiMaxLines)
		view.lines = view.plain[aiTextPrimary].appendLines(view.lines)
		view.height = 2*aiUserPadY + float32(len(view.lines))*aiBodyLine
		if len(item.attachments) > 0 {
			view.detail = aiAttachmentNames(item.attachments)
			view.height += aiAttachedLine
		}
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
	command, output := &view.plain[aiTextPrimary], &view.plain[aiTextOutput]
	command.reset("")
	output.reset("")
	cols := max(int((width-2*aiToolPad-12)/cell), 8)
	height := 2*aiToolPadY + aiToolHeader
	if view.detail != "" {
		command.layoutMono(view.detail, cols, aiMaxDetail, cell, cellHeight)
		view.lines = command.appendLines(view.lines)
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
			more := output.layoutMono(shown, cols, aiMaxOutput, cell, cellHeight)
			if more || len(shown) < len(item.result) {
				output.appendSynthetic("…")
			}
			view.lines = output.appendLines(view.lines)
			height += aiBlockHeight(len(output.lines), cellHeight, aiBoxPad) + aiToolAfterOut
		}
	}
	view.height = height
}

// aiScrollbarTheme keeps the narrow thumb visible against the panel at rest,
// and gives hover and press a clear contrast increase.
func (state *State) aiScrollbarTheme() gfx.Theme {
	theme := state.Theme()
	theme.Border = state.pal.faint
	theme.MutedForeground = state.pal.text
	return theme
}

// aiScrollbarValue reports the active list's position, formatted only when the
// visible percentage changes so a streaming repaint does not allocate for it.
func (state *State) aiScrollbarValue(history bool, offset, maximum float32) string {
	bar := &state.ai.panel.scrollbar
	percent := 0
	if maximum > 0 {
		percent = min(max(int(offset*100/maximum+0.5), 0), 100)
	}
	if bar.value == "" || bar.valueHistory != history || bar.percent != percent {
		bar.value = strconv.Itoa(percent) + "%"
		bar.percent, bar.valueHistory = percent, history
	}
	return bar.value
}

// aiPanelBeginPaint forgets the geometry of the previous paint, so a panel that
// is not painted again stops catching the wheel, and makes sure the workbench
// page will declare it.
func (state *State) aiPanelBeginPaint() {
	panel := &state.ai.panel
	panel.rect, panel.list, panel.listing = gfx.Rect{}, gfx.Rect{}, false
	panel.scrollbar.geometry = components.MessageScrollerGeometry{}
	if !state.aiPanelVisible() {
		panel.scrollbar.dragging = false
		panel.drawerOpen = false
	}
	panel.declared = false
	panel.menusShown = false
	panel.copyCount = 0
	panel.answer = gfx.Rect{}
	panel.linkRows, panel.linkBase = 0, 0
}

// aiPanelMode is how the last paint placed the panel: a card beside a section, a
// column the panes made room for, or a drawer over the window.
type aiPanelMode uint8

const (
	aiPanelSection aiPanelMode = iota
	aiPanelDocked
	aiPanelDrawer
)

// aiPanelResize is the edge the panel is dragged by: the window the handle moves
// in, where it sits in it, and how wide it is.
type aiPanelResize struct {
	window gfx.Rect
	ratio  float32
	handle float32
}

// declareAIPanel declares the assistant in the workbench page: one Part-named box
// over the window, holding the panel at the rect this window gives it and, above
// it, the open chip menus. The page names them, so pointer, key, wheel and text
// routing go through the page rather than a page of the panel's own.
func (state *State) declareAIPanel(c ui.C) {
	panel := &state.ai.panel
	if !state.aiPanelVisible() || panel.declared {
		return
	}
	panel.declared = true
	size := gfx.Size{Width: c.Bounds().Width, Height: c.Bounds().Height}
	if panel.contentDraw == nil {
		panel.contentDraw = state.paintAIPanelContent
		panel.overDraw = state.paintAIPanelChrome
	}
	c.Part(aiPanelPart)
	ui.Stack(c).Absolute().Inset(0).Children(func() {
		switch {
		case state.aiSectionReserve(size) > 0:
			panel.mode = aiPanelSection
			state.declareAISectionPanel(c, size)
		case state.aiReserve(state.deck.ActiveWorkspace(), size) > 0:
			panel.mode = aiPanelDocked
			state.declareAIDockedPanel(c, size)
		default:
			panel.mode = aiPanelDrawer
			state.declareAIDrawer(c, size)
		}
	})
}

// declareAISectionPanel declares the card beside a section: the rounded card the
// section makes room for, and its body inside it.
func (state *State) declareAISectionPanel(c ui.C, size gfx.Size) {
	panel, pal := &state.ai.panel, state.pal
	reserve := state.aiSectionReserve(size)
	bounds := gfx.Rect{
		X: size.Width - reserve + sectionCardMargin, Y: windowBarHeight,
		Width: reserve - 2*sectionCardMargin, Height: max(size.Height-windowBarHeight-sectionCardMargin-state.toolbarDockBottomReserve(false), 0),
	}
	bounds.X -= state.toolbarDockSideReserveFor(false, false)
	panel.rect = bounds
	window := gfx.Rect{Y: bounds.Y, Width: size.Width, Height: bounds.Height}
	dividerX := bounds.X - (sectionCardMargin+1)/2
	panel.resize = aiPanelResize{window: window, ratio: dividerX / max(window.Width-1, 1), handle: sectionCardMargin}
	ui.Column(c).Absolute().Left(bounds.X).Top(bounds.Y).Width(bounds.Width).Height(bounds.Height).
		Background(pal.workspace).Border(1, pal.windowStroke).Radius(radius.surface).Clip().Children(func() {
		state.declareAIPanelBody(c)
	})
}

// declareAIDockedPanel declares the column at the right of a workspace, where the
// panes left it room.
func (state *State) declareAIDockedPanel(c ui.C, size gfx.Size) {
	panel, pal := &state.ai.panel, state.pal
	reserve := state.aiReserve(state.deck.ActiveWorkspace(), size)
	inner := state.baseWorkspacePixelBounds(size)
	bounds := gfx.Rect{
		X: inner.X + inner.Width - reserve + 1, Y: windowBarHeight,
		Width: reserve - 1, Height: inner.Height,
	}
	panel.rect = bounds
	window := gfx.Rect{X: inner.X, Y: windowBarHeight, Width: inner.Width, Height: inner.Height}
	panel.resize = aiPanelResize{window: window, ratio: (inner.Width - reserve) / max(window.Width-1, 1), handle: 8}
	ui.Column(c).Absolute().Left(bounds.X).Top(bounds.Y).Width(bounds.Width).Height(bounds.Height).
		Background(pal.workspace).Children(func() {
		state.declareAIPanelBody(c)
	})
}

// declareAIDrawer declares the panel as a drawer over the window, as Viva's page
// Drawer: it paints its scrim and surface on a layer above the page's content
// and declares the body inside that layer, so a section painted after the page
// does not cover it.
func (state *State) declareAIDrawer(c ui.C, size gfx.Size) {
	panel := &state.ai.panel
	viewport := state.visibleWorkspacePixelBounds(size)
	extent := state.aiPanelExtent(size)
	drawerExtent := min(extent, size.Width*0.92)
	contentWidth := min(drawerExtent, viewport.Width)
	bounds := gfx.Rect{X: viewport.X + viewport.Width - contentWidth, Y: viewport.Y, Width: contentWidth, Height: viewport.Height}
	panel.rect = bounds
	panel.resize = aiPanelResize{window: viewport, ratio: (bounds.X - viewport.X) / max(viewport.Width-1, 1), handle: 8}
	panel.drawerOpen = true
	dismissed := ui.Drawer(c, &panel.drawerOpen, "", ui.DrawerSpec{
		Side: ui.DrawerRight, Size: drawerExtent, NonModal: true,
	}, func() {
		state.declareAIPanelBody(c)
	}, nil)
	if dismissed || !panel.drawerOpen {
		panel.drawerOpen = true // closing now; the next open starts open
		c.After(state.closeAIPanel)
	}
}

// paintAIPanel paints what the declared panel needs outside the page: its resize
// edge and the file-drop hint. The drawer keeps them inside its own layer, where
// the page's over hook painted them; the docked and section panels paint them at
// the page's layer, after it, as the panel always has.
func (state *State) paintAIPanel(canvas *gfx.Canvas, size gfx.Size) {
	panel := &state.ai.panel
	if !state.aiPanelVisible() || !panel.declared {
		return
	}
	switch panel.mode {
	case aiPanelDrawer:
		// A zero drawer scope does not take focus when it opens; ask for the
		// composer explicitly so typing reaches the assistant. The runtime applies
		// one request once, so later frames leave the focus the user chose alone.
		canvas.RequestFocus(hitAIInput)
	default:
		state.paintAIResize(canvas)
		state.paintAIDrop(canvas)
	}
}

// paintAIResize paints the panel's resize edge: only hover, press and focus show
// it, since the cards already carry their own border.
func (state *State) paintAIResize(canvas *gfx.Canvas) {
	panel := &state.ai.panel
	if panel.resize.window.Width <= 0 {
		return
	}
	theme := state.Theme()
	theme.Border = gfx.Color{}
	components.BeginResizable(canvas, theme, hitAIDivider, panel.resize.window, panel.resize.ratio).
		Limits(0, 1).HandleSize(panel.resize.handle).Paint(state.messages.AIPanel.ResizePanel)
}

// aiMenuOpen reports whether one of the chip menus is showing. A menu whose
// models or levels are gone paints nothing, so it is not open then either.
func (state *State) aiMenuOpen() bool {
	panel := &state.ai.panel
	if panel.reasoningMenu {
		if _, levels, ok := state.aiCurrentModelReasoning(); ok && len(levels) > 0 {
			return true
		}
	}
	if panel.autoMenu && state.ai.mode != agent.ModeAsk {
		if _, has := state.aiModelText(); has {
			return true
		}
	}
	if panel.modelMenu {
		if _, has := state.aiModelText(); has {
			return true
		}
	}
	return false
}

// aiMenuShown reports whether the workbench page's last paint declared an open
// chip menu. aiMenuOpen says one is wanted, but a menu with no models or levels
// paints no popup; only a shown menu owns the pointer and keys over the window.
func (state *State) aiMenuShown() bool {
	return state.aiMenuOpen() && state.ai.panel.menusShown
}

// aiMenuWidth is the assistant chip menu's width: what its words need plus room
// for the mark and the padding, between 140 and 320 pixels.
func aiMenuWidth(text float32) float32 { return min(max(text+56, 140), 320) }

// aiMenuContextWidth is the least width a menu with description rows may have:
// the same 144 a context menu keeps, which the approval and reasoning menus were
// before they became page menus.
const aiMenuContextWidth = float32(144)

// paintAIPanelContent paints the parts the declaration left for the last pass,
// once the page has laid the body out: the title buttons and setup card in their
// slots and the bottom's field and buttons. The list itself is the page's
// Control and paints in its own Draw (paintAIList).
func (state *State) paintAIPanelContent(d *ui.DrawContext) {
	canvas, panel, ai := d.Canvas, &state.ai.panel, &state.ai
	s := &panel.pageScratch
	state.paintAIHeader(canvas, &panel.body)
	if s.showSetup {
		components.BeginButton(canvas, state.Theme(), hitAISetup, panel.body.setup.Rect()).Small().Default(s.setupLabel)
	}
	// Keep the list's rectangle even when it is the empty state, which paints no
	// Control: the wheel and the scrollbar read it between paints.
	panel.list = panel.body.list.Rect()
	panel.listing = panel.history
	// A row that is gone takes the selection that pointed into it, whether the
	// list paints rows at all: the empty state has no Draw to do it later.
	panel.textSelect.Clamp(len(ai.items)*int(aiTextPartCount), panel.textLength)
	state.paintAIBottom(canvas, &panel.body)
}

// paintAIList is the list Control's Draw: the saved conversations or the
// conversation, painted with the rows' own layouts inside the control's
// rectangle. It is the last user of the frame-based row painters; they are not
// called from anywhere else.
func (state *State) paintAIList(d *ui.DrawContext) {
	canvas, panel, ai := d.Canvas, &state.ai.panel, &state.ai
	// The list slot is wider than the control by the divider's hit strip: the
	// rows are measured and painted from the slot, so their pixels do not move
	// when the control keeps clear of the resize handle.
	list := panel.list
	if list.Width <= 0 {
		list = d.Rect
	}
	panel.list = list
	panel.listing = panel.history
	panel.textFirst, panel.textLast = -1, -1
	switch {
	case panel.history:
		state.paintAIHistory(canvas, list)
	case len(ai.items) > 0:
		state.paintAIMessages(canvas, list)
	}
}

// paintAIPanelChrome paints what must come over the body's controls: the
// composer's resize grip, and, for the drawer, the panel's resize edge and the
// file-drop hint, which belong on the drawer's layer rather than the page's.
func (state *State) paintAIPanelChrome(d *ui.DrawContext) {
	canvas, panel := d.Canvas, &state.ai.panel
	if box := panel.composer; box.Width > 0 {
		// The box's top edge is what is dragged to make it taller.
		canvas.HitWithOptions(hitAIComposerGrip, gfx.Rect{X: box.X + 8, Y: box.Y - 3, Width: max(box.Width-16, 0), Height: 7}, state.messages.AIPanel.ResizeComposer,
			gfx.HitOptions{Role: gfx.RoleSlider, Cursor: gfx.PointerCursorResizeVertical, SkipFocus: true})
	}
	if panel.mode == aiPanelDrawer {
		state.paintAIResize(canvas)
		state.paintAIDrop(canvas)
	}
}

// declareAIPanelBody declares the panel's chrome in the container the workbench
// page gave it: the title row, the setup card, the divider and the message list,
// with the bottom under them. The icon buttons and the list content are painted
// into the slots once the page is laid out. The title row's controls read their
// click here, in the page's declaration.
func (state *State) declareAIPanelBody(c ui.C) {
	ai, panel := &state.ai, &state.ai.panel
	m := state.messages.AIPanel
	pal := state.pal
	width := panel.rect.Width - 2*aiPad
	s := &panel.pageScratch
	_, hasModel := state.aiModelText()
	s.setupLabel, s.setupSummary = m.SetupModel, m.SetupSummary
	s.showSetup = !hasModel || ai.needLogin != ""
	if ai.needLogin != "" {
		s.setupLabel, s.setupSummary = state.messages.AISettings.CLIAuthLogin, state.messages.AISettings.CLIAuthSignedOut
	}
	if canvas := c.Canvas(); canvas != nil {
		s.boxHeight = state.aiPrepareBottom(canvas, width)
		s.setupWidth = min(max(canvas.TextWidthWeight(s.setupLabel, aiSmallSize, gfx.WeightMedium)+32, 110), width*0.5)
	}
	if panel.composerDraw == nil {
		panel.composerDraw = state.paintAIComposerTextarea
	}
	if panel.listDraw == nil {
		panel.listDraw = state.paintAIList
		// The list's input is the application's own routing, as the terminal's
		// is: a shortcut, a modal dialog or another panel handler acts before
		// the conversation list's gestures, and the gestures are reached at the
		// point the window always reached them. WindowConfig routes the event
		// to this dispatcher because the control is the page's.
		panel.listHandlers.Pointer = state.Pointer
		panel.listHandlers.Key = state.Key
		panel.listHandlers.Scroll = state.Scroll
	}

	panel.body = aiBody{}
	ui.Column(c).Fill().Draw(panel.contentDraw).DrawOver(panel.overDraw).Children(func() {
		// The title row: the name, a new conversation, the history, and the way out.
		ui.Row(c).Height(aiTitleRow).PadX(aiPad).Gap(4).Items(ui.Center).Children(func() {
			ui.Icon(c, lucideIcons[iconBot]).Color(pal.accent).MarginSides(0, 4, 0, 0)
			ui.Text(c, m.Title).FontSize(metrics.FontSizeM).Bold().Color(pal.text).LineHeight(22).Grow(1)
			panel.body.newChat = ui.Slot(c).Size(barToolSize, barToolSize)
			panel.body.history = ui.Slot(c).Size(barToolSize, barToolSize)
			panel.body.close = ui.Slot(c).Size(barToolSize, barToolSize)
		})
		// The way to get a model; once there is one, the message box names it.
		if s.showSetup {
			ui.Row(c).Height(56).MarginSides(0, aiPad, 8, aiPad).PadX(12).Gap(12).Items(ui.Center).Background(pal.surface).Border(1, pal.line).Radius(radius.control).Children(func() {
				ui.Paragraph(c, s.setupSummary, 3).FontSize(aiSmallSize).Weight(gfx.WeightRegular).Color(pal.muted).LineHeight(aiSmallLine).Grow(1)
				panel.body.setup = ui.Slot(c).Size(s.setupWidth, 30)
			})
		}
		ui.Divider(c).Background(pal.line)
		// With nothing to show, the list's room is the page's own empty state;
		// the history and the conversation are one Control the page declares, so
		// its scroll gestures go to the control's handlers. The control starts at
		// the divider's hit strip so it does not cross the resize handle; the
		// strip is list padding, and the list is painted from the slot's whole
		// rectangle below.
		if panel.history || len(state.ai.items) > 0 {
			inset := panel.resize.handle/2 + 1
			panel.body.list = ui.Column(c).Grow(1).Children(func() {
				ui.Control(c, m.ListLabel, &panel.listHandlers).Fill().MarginLeft(inset).Draw(panel.listDraw)
			})
		} else {
			panel.body.list = state.declareAIEmpty(c)
		}
		state.declareAIBottom(c, &panel.body, s.boxHeight)
	})
	// The title row's controls are painted by hand into their slots; the page
	// reads their click while it declares them, so an event-pass paint delivers
	// it there rather than through the window's central dispatcher. The action
	// runs after the paint, as the central dispatcher did.
	if canvas := c.Canvas(); canvas != nil && state.modal == modalNone {
		switch {
		case canvas.Clicked(hitAIClose):
			c.After(state.closeAIPanel)
		case canvas.Clicked(hitAIHistory):
			c.After(state.toggleAIHistory)
		case canvas.Clicked(hitAINew) && state.ai.run == nil:
			c.After(state.newAIConversationFromPanel)
		}
	}
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
func (state *State) paintAIHeader(canvas *gfx.Canvas, body *aiBody) {
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

// declareAIEmpty declares the panel before the first question: a third of the
// free room above the icon, the title, the hint and the examples, and two thirds
// below.
func (state *State) declareAIEmpty(c ui.C) ui.Box {
	pal := state.pal
	title, hint, texts := state.aiEmptyTexts()
	return ui.Column(c).Grow(1).FillWidth().PadX(aiPad).Clip().Children(func() {
		ui.Spacer(c).MinHeight(8)
		ui.Column(c).Shrink(0).Children(func() {
			ui.Icon(c, lucideIcons[iconBot]).Size(26, 26).Color(pal.accent).Self(ui.Center)
			ui.Text(c, title).FontSize(15).Bold().Color(pal.text).LineHeight(24).TextCenter().MarginSides(14, 0, 0, 0)
			ui.Paragraph(c, hint, 4).FontSize(aiSmallSize).Weight(gfx.WeightRegular).Color(pal.muted).LineHeight(aiSmallLine).TextCenter().MarginSides(8, 12, 0, 12)
			ui.Column(c).Gap(6).MarginSides(16, 0, 6, 0).Children(func() {
				for index, example := range texts {
					state.declareAIExample(c, index, example)
				}
			})
		})
		ui.Spacer(c).Grow(2)
	})
}

// declareAIExample declares one prompt of the empty panel as a row a click
// fills in.
func (state *State) declareAIExample(c ui.C, index int, example string) {
	pal, id := state.pal, hitAIExamples.At(index)
	background := pal.surface
	if canvas := c.Canvas(); canvas != nil {
		switch {
		case canvas.Pressed() == id:
			background = pal.surfaceHot
		case canvas.Hovered() == id:
			background = pal.surfaceSoft
		}
	}
	ui.Row(c).Height(36).PadSides(0, 10, 0, 12).Gap(8).Items(ui.Center).Background(background).Radius(radius.control).
		HitWith(id, example, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {
		ui.Text(c, example).FontSize(aiSmallSize).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(aiSmallLine).Ellipsis().Grow(1)
		ui.Icon(c, lucideIcons[iconChevronRight]).Size(14, 14).Color(pal.muted).Shrink(0)
	})
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
func (state *State) paintAIHistory(canvas *gfx.Canvas, list gfx.Rect) {
	ai, panel, pal := &state.ai, &state.ai.panel, state.pal
	m := state.messages.AIPanel
	var body ui.Box
	f := state.aiFlex.history.Begin(canvas, state.Theme(), list)
	f.Column().Fill().Children(func() {
		f.Column().Height(aiHistoryHeader).Shrink(0).PadSides(8, aiPad, 0, aiPad).Children(func() {
			f.Text(m.HistoryTitle).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(22)
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

	scroller := components.BeginMessageScroller(canvas, state.aiScrollbarTheme(), area, content, panel.historyTop).
		Scrollbar(hitAIHistoryBar, m.HistoryScrollbarLabel, state.aiScrollbarValue(true, panel.historyTop, panel.maxTop))
	scroller.BeginContent()
	for offset, slot := range panel.rowSlots {
		index := first + offset
		conversation := &ai.conversations[index]
		openID, deleteID := hitAIHistoryOpen.At(index), hitAIHistoryDelete.At(index)
		renameID := hitAIHistoryRename.At(index)
		var background gfx.Color
		switch {
		case canvas.Pressed() == openID:
			background = pal.surfaceHot
		case canvas.Hovered() == openID || canvas.Hovered() == deleteID || canvas.Hovered() == renameID:
			background = pal.surfaceSoft
		case conversation.ID == ai.conv:
			background = pal.accentSoft
		}
		var remove, rename ui.Box
		r := state.aiFlex.row.Begin(canvas, state.Theme(), slot.Rect())
		r.Row().Fill().PadSides(0, 4, 0, 10).Gap(10).Items(ui.Center).Background(background).Radius(radius.control).
			HitWith(openID, conversation.Title, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.HistoryOpen}).Children(func() {
			r.Column().Grow(1).Children(func() {
				r.Text(conversation.Title).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(20).Ellipsis()
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
	panel.scrollbar.geometry = scroller.EndContent()
}

// intersectRect is the part of a that lies inside b.
func intersectRect(a, b gfx.Rect) gfx.Rect {
	left, top := max(a.X, b.X), max(a.Y, b.Y)
	right, bottom := min(a.X+a.Width, b.X+b.Width), min(a.Y+a.Height, b.Y+b.Height)
	if right <= left || bottom <= top {
		return gfx.Rect{}
	}
	return gfx.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

// paintAIMessages draws the conversation in a scrolling list. Every message keeps
// its measured height (aiRowView), so where each one starts is a running sum and a
// long conversation costs the rows on screen only: those are declared in a flex
// Scroll between two blocks as tall as the rows above and below them, and flex
// places them.
func (state *State) paintAIMessages(canvas *gfx.Canvas, list gfx.Rect) {
	ai, panel := &state.ai, &state.ai.panel
	m := state.messages.AIPanel
	rowW := list.Width - 2*aiPad
	cell, cellHeight := canvas.MonoCellSize(aiMonoSize, state.terminalFontFace)
	status, usage := state.aiRunStatusText()
	statusHeight := float32(0)
	if status != "" {
		panel.statusLines = canvas.WrapLines(panel.statusLines[:0], status, aiSmallSize, gfx.WeightRegular, max(rowW-20, 1), 4)
		panel.usageLines = panel.usageLines[:0]
		statusHeight = float32(len(panel.statusLines)) * aiSmallLine
		if usage != "" {
			panel.usageLines = canvas.WrapLines(panel.usageLines, usage, aiSmallSize, gfx.WeightRegular, max(rowW-20, 1), 8)
			statusHeight += float32(len(panel.usageLines))*aiSmallLine + 4
		}
	}

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
			ends := panel.selectionEnds(index)
			state.aiBuild(canvas, view, item, rowW, cell, cellHeight, title)
			view.actions = lastAnswer
			if lastAnswer {
				view.height += aiAnswerAction
			}
			panel.keepSelectionEnds(index, ends)
		}
		view.top = y
		y += view.height + aiGap
	}
	// A rebuilt row may be shorter than the text a selection pointed into.
	panel.textSelect.Clamp(panel.textSurfaces(), panel.textLength)
	content := y
	if statusHeight > 0 {
		content += statusHeight + aiGap
	}
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
	panel.textFirst, panel.textLast = first, last

	// The rows are placed by a Scroll of slots, then declared one by one into them
	// under the clip of the viewport, so that a row's hits are followed by its
	// copy buttons, in the order the keyboard visits them.
	panel.rowSlots = panel.rowSlots[:0]
	var statusSlot ui.Box
	f := state.aiFlex.messages.Begin(canvas, state.Theme(), list)
	f.Scroll(scroll).Fill().PadX(aiPad).Children(func() {
		if first < 0 {
			f.Column().Height(y)
		} else {
			f.Column().Height(panel.rows[first].top)
			for index := first; index <= last; index++ {
				panel.rowSlots = append(panel.rowSlots, f.Slot().Height(panel.rows[index].height).MarginSides(0, 0, aiGap, 0))
			}
			end := &panel.rows[last]
			f.Column().Height(y - (end.top + end.height + aiGap))
		}
		if statusHeight > 0 {
			statusSlot = f.Slot().Height(statusHeight).MarginSides(0, 0, aiGap, 0)
		}
	})
	f.End()

	scroller := components.BeginMessageScroller(canvas, state.aiScrollbarTheme(), list, content, scroll).
		Scrollbar(hitAIMessageBar, m.ScrollbarLabel, state.aiScrollbarValue(false, scroll, panel.maxScroll)).Buttons(gfx.ID{}, "", hitAIToEnd, m.ScrollToEnd)
	scroller.BeginContent()
	for offset, slot := range panel.rowSlots {
		index := first + offset
		view, item := &panel.rows[index], &ai.items[index]
		panel.copies = panel.copies[:0]
		r := state.aiFlex.row.Begin(canvas, state.Theme(), slot.Rect())
		body := state.declareAIRow(r, index, view, item)
		r.End()
		state.paintAIPlainTextSelections(canvas, list, view, index)
		switch {
		case item.kind == aiAssistant:
			state.paintAIAssistant(canvas, body.Rect(), list, view, index)
		case item.kind == aiReasoning && state.aiThinking(index), item.kind == aiTool && item.status == aiToolRunning:
			// Still thinking, or the command is running: a spinner in the slot the row left for it.
			components.PaintSpinner(canvas, state.Theme(), body.Rect(), components.SpinnerPhase(canvas.Now()/spinnerStep))
			canvas.RequestFrameIn(spinnerStep)
		}
	}
	if statusHeight > 0 {
		label := status
		if usage != "" {
			label += "\n" + usage
		}
		state.paintAIRunStatus(canvas, statusSlot.Rect(), label)
	}
	panel.scrollbar.geometry = scroller.EndContent()
}

func (state *State) paintAIRunStatus(canvas *gfx.Canvas, bounds gfx.Rect, label string) {
	panel, pal := &state.ai.panel, state.pal
	var activity ui.Box
	f := state.aiFlex.status.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Gap(4).HitWith(hitAIRunStatus, label, gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true}).Children(func() {
		f.Row().Gap(8).Children(func() {
			activity = f.Slot().Size(12, 12).Shrink(0).MarginSides(2, 0, 0, 0)
			f.Column().Grow(1).Children(func() {
				aiTextLines(f, panel.statusLines, aiSmallSize, aiSmallLine, pal.muted)
			})
		})
		if len(panel.usageLines) > 0 {
			f.Column().Children(func() {
				aiTextLines(f, panel.usageLines, aiSmallSize, aiSmallLine, pal.muted)
			})
		}
	})
	f.End()
	if state.aiRunActive() {
		components.PaintSpinner(canvas, state.Theme(), activity.Rect(), components.SpinnerPhase(canvas.Now()/spinnerStep))
		canvas.RequestFrameIn(spinnerStep)
	} else {
		icon := iconCircleCheck
		if state.ai.lastRun != nil && state.ai.lastRun.phase != aiRunCompleted {
			icon = iconBot
		}
		canvas.DrawIcon(lucideIcons[icon], activity.Rect(), pal.muted)
	}
}

// aiThinking reports whether row index is a thought the model is still writing:
// the last row of a conversation that has a request running.
func (state *State) aiThinking(index int) bool {
	ai := &state.ai
	return ai.run != nil && index == len(ai.items)-1 && ai.items[index].kind == aiReasoning
}

// aiRowFrame is the part of the frame API the conversation rows are declared
// with. The list is a page Control whose Draw paints into the canvas; naming
// only the methods the rows use keeps the row painters independent of the
// concrete frame type.
type aiRowFrame interface {
	Column() ui.Box
	Icon(gfx.Icon) ui.Box
	Row() ui.Box
	Slot() ui.Box
	Spacer() ui.Box
	Stack() ui.Box
	Text(string) ui.Box
}

// aiTextLines declares wrapped lines of body text, one element each.
func aiTextLines(f aiRowFrame, lines []string, size, lineHeight float32, color gfx.Color) {
	for _, line := range lines {
		f.Text(line).FontSize(size).Weight(gfx.WeightRegular).Color(color).LineHeight(lineHeight)
	}
}

// aiMonoLines declares lines in the terminal's font.
func (state *State) aiMonoLines(f aiRowFrame, lines []string, color gfx.Color) {
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
func (state *State) declareAIRow(f aiRowFrame, index int, view *aiRowView, item *aiItem) ui.Box {
	pal, m := state.pal, state.messages.AIPanel
	row := f.Column()
	var body ui.Box
	view.textSlotSet = [aiTextPartCount]bool{}
	switch item.kind {
	case aiUser:
		row.Items(ui.End).Children(func() {
			f.Column().MaxWidth(view.width).PadSides(aiUserPadY, aiUserPadX, aiUserPadY, aiUserPadX).Background(pal.accentSoft).Radius(radius.popup).Children(func() {
				text := f.Column().Children(func() {
					aiTextLines(f, view.lines, aiBodySize, aiBodyLine, pal.text)
				})
				view.textSlots[aiTextPrimary] = text
				view.textSlotSet[aiTextPrimary] = true
				if index < aiMaxRowHits {
					text.HitWith(hitAIAnswerText.At(index), item.text, gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true})
				}
				if len(item.attachments) > 0 {
					// What the question was sent with, under its words.
					f.Row().Height(aiAttachedLine).Gap(5).Items(ui.Center).Children(func() {
						f.Icon(lucideIcons[iconPaperclip]).Size(12, 12).Color(pal.muted).Shrink(0)
						f.Text(view.detail).FontSize(aiSmallSize).Color(pal.muted).LineHeight(aiSmallLine).Ellipsis().Shrink(1)
					})
				}
			})
		})
	case aiTargetChange:
		row.Height(view.height).Items(ui.Center).Children(func() {
			f.Row().Fill().Gap(10).Items(ui.Center).Children(func() {
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
			header := f.Row().Height(aiReasonHeader).PadX(4).Gap(6).Items(ui.Center).Radius(radius.item).Children(func() {
				icon := iconChevronRight
				if view.expanded {
					icon = iconChevronDown
				}
				if state.aiThinking(index) {
					body = f.Slot().Size(12, 12)
				} else {
					f.Icon(lucideIcons[icon]).Size(12, 12).Color(pal.muted)
				}
				f.Text(view.title).FontSize(aiSmallSize).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(18)
			})
			if index < aiMaxRowHits {
				header.Hover(ui.Look{}.Background(pal.surfaceSoft)).HitWith(hitAIRows.At(index), view.title, gfx.HitOptions{Role: gfx.RoleButton, Expanded: view.expanded})
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
			f.Row().Height(aiToolHeader).Items(ui.Center).Children(func() {
				f.Icon(lucideIcons[aiToolIcon(item.call.Name)]).Size(14, 14).Color(pal.muted).MarginSides(0, 8, 0, 0)
				f.Text(view.title).FontSize(aiSmallSize).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(20).Ellipsis()
				if item.call.Name == tools.NameTerminalExec || item.call.Name == tools.NameTerminalExecMany {
					foreground, fill := state.aiRiskColors(item.risk)
					f.Row().Height(16).PadX(6).Center().Shrink(0).MarginSides(0, 0, 0, 8).Background(fill).Radius(radius.control).Children(func() {
						f.Text(state.aiRiskText(item.risk)).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground).LineHeight(14)
					})
				}
				f.Spacer().MinWidth(8)
				f.Row().Shrink(0).Items(ui.Center).Children(func() {
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
					f.Text(statusText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(statusColor).LineHeight(aiToolHeader)
				})
			})
			if view.commandLines > 0 {
				f.Column().Pad(aiBoxPad).MarginSides(0, 0, aiToolAfterCmd, 0).Background(pal.terminal).Radius(radius.item).Children(func() {
					text := f.Column().Children(func() {
						state.aiMonoLines(f, view.lines[:view.commandLines], pal.text)
					})
					view.textSlots[aiTextPrimary] = text
					view.textSlotSet[aiTextPrimary] = true
					if index < aiMaxRowHits {
						text.HitWith(hitAIAnswerText.At(index), view.detail, gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true})
					}
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
			strip := f.Row().Height(aiToolHeader).MarginSides(0, -4, 0, -4).PadX(4).Gap(6).Items(ui.Center).Radius(radius.item).Children(func() {
				f.Icon(lucideIcons[icon]).Size(12, 12).Color(pal.muted)
				f.Text(label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).LineHeight(16).Shrink(0).MarginSides(0, 4, 0, 0)
				if !view.expanded && view.excerpt != "" {
					f.Text(view.excerpt).FontSize(metrics.FontSizeS).MonoFace(state.terminalFontFace).Color(pal.faint)
				}
			})
			if index < aiMaxRowHits {
				strip.Hover(ui.Look{}.Background(pal.surfaceSoft)).HitWith(hitAIRows.At(index), view.title, gfx.HitOptions{
					Role: gfx.RoleButton, Expanded: view.expanded, Value: statusText, Hint: label,
				})
			}
			if view.expanded {
				f.Column().Pad(aiBoxPad).MarginSides(0, 0, aiToolAfterOut, 0).Background(pal.terminal).Radius(radius.item).Children(func() {
					text := f.Column().Children(func() {
						state.aiMonoLines(f, view.lines[view.commandLines:], pal.muted)
					})
					view.textSlots[aiTextOutput] = text
					view.textSlotSet[aiTextOutput] = true
					if index < aiMaxRowHits {
						text.HitWith(hitAIToolOutputText.At(index), view.plain[aiTextOutput].source, gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true})
					}
				})
			}
		})
	}
	if item.kind == aiAssistant || item.kind == aiReasoning && state.aiThinking(index) || item.kind == aiTool && item.status == aiToolRunning {
		return body
	}
	return row
}

// paintAIPlainTextSelections records the text geometry flex just produced and
// paints the selected user or tool block. Selection uses the assistant's color.
func (state *State) paintAIPlainTextSelections(canvas *gfx.Canvas, list gfx.Rect, view *aiRowView, index int) {
	panel := &state.ai.panel
	for part := range aiTextPartCount {
		if view.textSlotSet[part] {
			view.plain[part].canvas = canvas
			view.plain[part].rect = view.textSlots[part].Rect()
		}
	}
	if panel.textSelect.Empty() {
		return
	}
	canvas.Clip(list)
	defer canvas.Unclip()
	for part := range aiTextPartCount {
		if !view.textSlotSet[part] {
			continue
		}
		if selected := panel.textSelect.In(aiTextSurface(index, part)); selected.Anchor != selected.Focus {
			view.plain[part].paintSelection(canvas, selected, withAlpha(state.pal.text, 0.25))
		}
	}
}

// paintAIAssistant paints an assistant message, which is Markdown, in the room
// flex gave it, then the copy buttons of its code blocks over it.
func (state *State) paintAIAssistant(canvas *gfx.Canvas, bounds, list gfx.Rect, view *aiRowView, index int) {
	pal, m := state.pal, state.messages.AIPanel
	panel := &state.ai.panel
	look := components.MarkdownLookFor(state.Theme())
	blue := pal.ansi[4] // the accent is nearly white; a link has to stand out from the text
	look.Text, look.Muted, look.Link = pal.text, pal.muted, gfx.RGB(blue[0], blue[1], blue[2])
	look.CodeFill, look.InlineCodeFill, look.Line, look.Radius = pal.terminal, pal.surfaceSoft, pal.line, radius.control
	// MarkdownLookFor tints a selection with theme.Primary at 50%, and this
	// palette's accent is its text colour: on the dark palette that is a grey
	// band the selected text barely stands out from. Use the terminal's
	// selection recipe instead, so both places look the same.
	look.Selection = withAlpha(pal.text, 0.25)
	ansi := func(index int) gfx.Color { return gfx.RGB(pal.ansi[index][0], pal.ansi[index][1], pal.ansi[index][2]) }
	look.Code = [components.MarkdownCodeClasses]gfx.Color{
		components.CodeComment: pal.faint, components.CodeString: ansi(2), components.CodeNumber: ansi(3),
		components.CodeKeyword: ansi(5), components.CodeFunction: ansi(4), components.CodeVariable: ansi(6),
		components.CodeFlag: ansi(6), components.CodeKey: ansi(4), components.CodeAdded: ansi(2), components.CodeRemoved: ansi(1),
	}
	if panel.linkRows < len(panel.linkTable) {
		panel.linkTable[panel.linkRows] = aiLinkRow{base: panel.linkBase, row: index}
		panel.linkRows++
	}
	text := gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: min(bounds.Height, view.md.Height())}
	view.textRect = text
	if index < aiMaxRowHits {
		canvas.HitWithOptions(hitAIAnswerText.At(index), text, m.AnswerText, gfx.HitOptions{Role: gfx.RoleGroup, SkipTab: true})
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

func (state *State) paintAIAnswerActions(canvas *gfx.Canvas, bounds, list gfx.Rect, index int) {
	panel, pal := &state.ai.panel, state.pal
	m := state.messages.AIPanel
	panel.answer = bounds
	panel.answerOver = pointInRect(panel.pointer.X, panel.pointer.Y, bounds)
	action := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - aiAnswerAction, Width: bounds.Width, Height: aiAnswerAction}
	if visible := intersectRect(action, list); visible.Height < action.Height {
		return
	}
	if !pointInRect(panel.pointer.X, panel.pointer.Y, bounds) {
		return
	}
	item := &state.ai.items[index]
	button := gfx.Rect{X: action.X, Y: action.Y + 3, Width: 20, Height: 20}
	icon := iconCopy
	if panel.copied == item.text {
		if left := aiCopiedFor - (state.now() - panel.copiedAt); left > 0 {
			icon = iconCheck
			canvas.RequestFrameIn(left)
		}
	}
	state.paintIconButton(canvas, hitAICopyAnswer, icon, button, m.CopyAnswer, false, iconButtonQuiet, pal.workspace)
}

// aiCopySlot is where the copy button of a code block goes, once flex has placed it.
type aiCopySlot struct {
	slot ui.Box
	code string
}

// aiFlexViews are the layouts the assistant's panel is declared with.
type aiFlexViews struct {
	history, historyRows ui.View
	messages             ui.View
	row                  ui.View // one row of the history or of the conversation, declared into its slot
	status               ui.View
}
