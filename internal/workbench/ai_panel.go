package workbench

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	layoutcore "motty/internal/workspace"
)

// The assistant's panel: a column at the right edge of a terminal workspace.
// This file is its behaviour (state, events, layout reservation); the painting
// is in ai_panel_paint.go.
//
// Hit identities are names in the assistant panel's place of the tree; its lists
// are scopes whose rows are numbered items.
//
// aiPanelHits is the place of the controls this file paints.
var aiPanelHits = tree.Feature("ai-panel")

var (
	hitAIToggle           = tree.ID(aiPanelHits, "ai-toggle")
	hitAIClose            = tree.ID(aiPanelHits, "ai-close")
	hitAINew              = tree.ID(aiPanelHits, "ai-new")
	hitAIHistory          = tree.ID(aiPanelHits, "ai-history")
	hitAIMode             = tree.ID(aiPanelHits, "ai-mode")
	hitAISetup            = tree.ID(aiPanelHits, "ai-setup")
	hitAISend             = tree.ID(aiPanelHits, "ai-send")
	hitAIInput            = tree.ID(aiPanelHits, "ai-input")
	hitAIConfirm          = tree.ID(aiPanelHits, "ai-confirm")
	hitAIApprove          = tree.ID(aiPanelHits, "ai-approve")
	hitAIDecline          = tree.ID(aiPanelHits, "ai-decline")
	hitAIToEnd            = tree.ID(aiPanelHits, "ai-to-end")
	hitAIMessageBar       = tree.ID(aiPanelHits, "ai-message-scroll-bar")
	hitAIHistoryBar       = tree.ID(aiPanelHits, "ai-history-scroll-bar")
	hitAIDrawerDismiss    = tree.ID(aiPanelHits, "ai-drawer-dismiss")
	hitAIModelChip        = tree.ID(aiPanelHits, "ai-model-chip")
	hitAIDivider          = tree.ID(aiPanelHits, "ai-divider")
	hitAIComposerGrip     = tree.ID(aiPanelHits, "ai-composer-grip")
	hitAIModelDismiss     = tree.ID(aiPanelHits, "ai-model-dismiss")
	hitAIModelSubDismiss  = tree.ID(aiPanelHits, "ai-model-submenu-dismiss")
	hitAIAutoScope        = tree.ID(aiPanelHits, "ai-auto-scope")
	hitAIAutoScopeDismiss = tree.ID(aiPanelHits, "ai-auto-scope-dismiss")
	hitAIReasoningChip    = tree.ID(aiPanelHits, "ai-reasoning-chip")
	hitAIReasoningDismiss = tree.ID(aiPanelHits, "ai-reasoning-dismiss")
	hitAIReasoningMenu    = tree.Sub(aiPanelHits, tree.Name("ai-reasoning-menu"))
	hitAIModelList        = tree.ID(aiPanelHits, "ai-model-list") // names the models menu for its entrance and exit; it is not a control
	hitAIModelBar         = tree.ID(aiPanelHits, "ai-model-scroll-bar")
	hitAIRunStatus        = tree.ID(aiPanelHits, "ai-run-status")
)

const (
	aiExamples      = 3
	aiMaxHistory    = 50
	aiMaxRowHits    = 3000
	aiMaxCopyBlocks = 32
	aiMaxLinkRows   = 64
	aiMenuRows      = 10   // models the chip's menu shows at once
	aiMenuModels    = 1000 // and the most it lists for one provider
)

var (
	hitAIExamples        = tree.Sub(aiPanelHits, tree.Name("ai-examples"))       // the example prompts
	hitAIHistoryOpen     = tree.Sub(aiPanelHits, tree.Name("ai-history-open"))   // saved conversations, to open
	hitAIHistoryDelete   = tree.Sub(aiPanelHits, tree.Name("ai-history-delete")) // and to delete
	hitAIHistoryRename   = tree.Sub(aiPanelHits, tree.Name("ai-history-rename")) // and to rename
	hitAIRenameClose     = tree.ID(aiPanelHits, "ai-rename-close")
	hitAIRenameCancel    = tree.ID(aiPanelHits, "ai-rename-cancel")
	hitAIRenameSubmit    = tree.ID(aiPanelHits, "ai-rename-submit")
	hitAIRenameField     = tree.ID(aiPanelHits, "ai-rename-field")
	hitAIRows            = tree.Sub(aiPanelHits, tree.Name("ai-rows"))              // collapsible rows of the conversation
	hitAICopies          = tree.Sub(aiPanelHits, tree.Name("ai-copies"))            // copy buttons of code blocks
	hitAIAnswerText      = tree.Sub(aiPanelHits, tree.Name("ai-answer-text"))       // selectable primary text of a conversation row
	hitAIToolOutputText  = tree.Sub(aiPanelHits, tree.Name("ai-tool-output-text"))  // selectable output of a tool row
	hitAICopyAnswer      = tree.ID(aiPanelHits, "ai-copy-answer")                   // copy the last answer
	hitAILinks           = tree.Sub(aiPanelHits, tree.Name("ai-links"))             // links in the assistant's answers
	hitAIMentions        = tree.Sub(aiPanelHits, tree.Name("ai-mentions"))          // sessions an @ can name
	hitAIModels          = tree.Sub(aiPanelHits, tree.Name("ai-models"))            // the models of the provider the chip menu has open
	hitAIModelProviders  = tree.Sub(aiPanelHits, tree.Name("ai-model-providers"))   // the providers the chip menu lists
	hitAIAutoScopeLevels = tree.Sub(aiPanelHits, tree.Name("ai-auto-scope-levels")) // the scopes the approval chip offers
)

// The terminal menu's two assistant items.
var (
	hitTerminalMenuAsk     = tree.ID(terminalMenuHits, "ask")
	hitTerminalMenuExplain = tree.ID(terminalMenuHits, "explain")
)

const (
	aiPanelWidth  = float32(380)            // the preferred width of the docked panel
	aiPanelMin    = float32(300)            // the least it can be dragged to
	aiDockAt      = float32(900)            // narrower windows get a drawer instead
	aiCopiedFor   = 1500 * time.Millisecond // how long a copy button shows its check
	aiMinTerminal = float32(360)            // the panes keep at least this much beside the panel
)

func inAIPanel(id gfx.ID) bool { return tree.Within(id.Scope(), aiPanelHits) }

type aiDragKind uint8

const (
	aiDragNone aiDragKind = iota
	aiDragWidth
	aiDragHeight
)

// aiLinkRow is a message whose links were painted: its first hit number.
type aiLinkRow struct{ base, row int }

// aiTextPart identifies one independently selectable block in a conversation row.
type aiTextPart uint8

const (
	aiTextPrimary aiTextPart = iota // an answer, a user message, or a tool's command
	aiTextOutput                    // a tool's expanded output
	aiTextPartCount
)

// aiTextLayout is the selection contract shared by Markdown answers and the
// plain-text blocks of user messages and tool cards. Offsets are source bytes.
type aiTextLayout interface {
	TextLen() int
	SelectedText(components.MarkdownSelection) string
	OffsetAt(gfx.Point) (int, bool)
}

// aiTextSurface numbers a block of conversation text in reading order, which is
// how a selection spans several: the rows from the top, and in a row its blocks
// in the order of aiTextPart. A row without a block still has its numbers.
func aiTextSurface(row int, part aiTextPart) int { return row*int(aiTextPartCount) + int(part) }

// aiTextSurfaceRow is the row and block a number stands for.
func aiTextSurfaceRow(surface int) (row int, part aiTextPart) {
	return surface / int(aiTextPartCount), aiTextPart(surface % int(aiTextPartCount))
}

// textSurfaces is how many numbers the conversation's blocks use.
func (panel *aiPanel) textSurfaces() int { return len(panel.rows) * int(aiTextPartCount) }

// textLength is the length of one block's selectable text, 0 for a block a row
// does not have.
func (panel *aiPanel) textLength(surface int) int {
	row, part := aiTextSurfaceRow(surface)
	if row < 0 || row >= len(panel.rows) {
		return 0
	}
	if layout := panel.rows[row].textLayout(part); layout != nil {
		return layout.TextLen()
	}
	return 0
}

// clearAITextSelection drops the selection, a drag in progress included.
func (panel *aiPanel) clearAITextSelection() {
	panel.textSelect.Clear()
}

// selectionFor is the Markdown highlight to paint for one row, empty for every
// other row and for plain-text blocks, which have their own painter.
func (panel *aiPanel) selectionFor(index int) components.MarkdownSelection {
	return panel.textSelect.In(aiTextSurface(index, aiTextPrimary))
}

// hasAITextSelection reports whether there is text to copy.
func (panel *aiPanel) hasAITextSelection() bool {
	return !panel.textSelect.Empty()
}

// aiTextSelectionText is the selected source text, when it is still there. A
// selection that runs through several messages copies each one's part, a blank
// line between them.
func (panel *aiPanel) aiTextSelectionText() (string, bool) {
	if !panel.hasAITextSelection() {
		return "", false
	}
	text := panel.textSelect.Text(panel.textSurfaces(), "\n\n", func(surface int, part components.MarkdownSelection) string {
		row, block := aiTextSurfaceRow(surface)
		if layout := panel.rows[row].textLayout(block); layout != nil {
			return layout.SelectedText(part)
		}
		return ""
	})
	return text, text != ""
}

// aiSelectionEnds is the text of the blocks of one row that hold an end of the
// selection, taken before the row is rebuilt.
type aiSelectionEnds [2]struct {
	held bool
	text string
}

// selectionEnds records the blocks of row that hold the selection's ends. A
// selection indexes source text, so an end survives a rebuild only when the new
// block still begins with the old one.
func (panel *aiPanel) selectionEnds(row int) (ends aiSelectionEnds) {
	if panel.textSelect.Empty() {
		return ends
	}
	for end, point := range [2]components.TextPoint{panel.textSelect.Anchor, panel.textSelect.Focus} {
		if pointRow, part := aiTextSurfaceRow(point.Surface); pointRow == row && row < len(panel.rows) {
			if layout := panel.rows[row].textLayout(part); layout != nil {
				ends[end].held = true
				ends[end].text = layout.SelectedText(components.MarkdownSelection{Focus: layout.TextLen()})
			}
		}
	}
	return ends
}

// keepSelectionEnds clears the selection when a rebuilt row no longer begins
// with the text an end of the selection pointed into.
func (panel *aiPanel) keepSelectionEnds(row int, ends aiSelectionEnds) {
	for end, point := range [2]components.TextPoint{panel.textSelect.Anchor, panel.textSelect.Focus} {
		if !ends[end].held {
			continue
		}
		_, part := aiTextSurfaceRow(point.Surface)
		layout := panel.rows[row].textLayout(part)
		if layout == nil || !strings.HasPrefix(layout.SelectedText(components.MarkdownSelection{Focus: layout.TextLen()}), ends[end].text) {
			panel.clearAITextSelection()
			return
		}
	}
}

// textPointAt is where a window point lies in the conversation's text, for a
// selection that is being dragged: the block under it, or else the nearest
// painted block, so a drag that leaves a message or crosses the gap between two
// goes on into the next. Above a block it is the block's start and below it its
// end.
func (panel *aiPanel) textPointAt(point gfx.Point) (components.TextPoint, bool) {
	var (
		best     components.TextPoint
		found    bool
		bestDist float32
	)
	for row := panel.textFirst; row >= 0 && row <= panel.textLast && row < len(panel.rows); row++ {
		view := &panel.rows[row]
		for part := aiTextPart(0); part < aiTextPartCount; part++ {
			bounds, painted := view.textBounds(part)
			layout := view.textLayout(part)
			if !painted || layout == nil {
				continue
			}
			dy := max(bounds.Y-point.Y, point.Y-(bounds.Y+bounds.Height), 0)
			dx := max(bounds.X-point.X, point.X-(bounds.X+bounds.Width), 0)
			dist := dy*1_000_000 + dx
			if found && dist >= bestDist {
				continue
			}
			offset := 0
			switch {
			case point.Y < bounds.Y:
			case point.Y > bounds.Y+bounds.Height:
				offset = layout.TextLen()
			default:
				at, ok := layout.OffsetAt(point)
				if !ok {
					continue
				}
				offset = at
			}
			best, bestDist, found = components.TextPoint{Surface: aiTextSurface(row, part), Offset: offset}, dist, true
		}
	}
	return best, found
}

// aiScrollbarState is the active list's painted geometry, accessible value and
// pointer gesture. dragGeometry stays fixed while content streams beneath it.
type aiScrollbarState struct {
	geometry     components.MessageScrollerGeometry
	dragGeometry components.MessageScrollerGeometry
	value        string
	percent      int
	valueHistory bool
	dragging     bool
	history      bool
	grab         float32
}

// aiPanel is what the panel keeps beyond the conversation itself: layout caches,
// the geometry of the last paint and small view state. It is all derived or
// cosmetic; nothing here is saved.
type aiPanel struct {
	targetOpen                  bool
	targetBounds                gfx.Rect
	targetTitle, targetValue    string
	targetWidth, targetFontSize float32
	rows                        []aiRowView

	// history shows the saved conversations in place of the messages.
	history      bool
	historyDates []string
	historyTop   float32

	// focusBack asks the next paint to put the keyboard back in the terminal
	// after the panel closed from inside itself, which a trapped drawer used to
	// do for the user.
	focusBack bool

	// Geometry of the last paint, for the wheel and the text input.
	rect      gfx.Rect // the whole panel; empty when it was not painted
	list      gfx.Rect // the message list (or the history list)
	listing   bool     // list is the history list
	maxScroll float32  // largest scroll offset of the message list
	maxTop    float32  // and of the history list

	// fitted is the width the terminals were last sized for.
	fitted float32

	input app.TextInput // the native editor of the message box, reused

	// dropping is whether files are being dragged over the panel; chips are the
	// waiting attachments' places above the message box, chipRows their rows.
	dropping bool
	chips    []aiChip
	chipRows int

	copyTargets [aiMaxCopyBlocks]string
	// rowSlots and copies are where flex put the rows on screen and, in the row
	// being painted, the copy buttons of its code blocks. They are only valid
	// during the paint that filled them and are emptied at the start of the next.
	rowSlots  []ui.Box
	copies    []aiCopySlot
	copyCount int
	copied    string // the code last copied, so its button shows it
	copiedAt  time.Duration

	// linkTable says which message each run of link hits of the last paint
	// belongs to: hit number n is in the row whose base is the largest not above n.
	linkTable [aiMaxLinkRows]aiLinkRow
	linkRows  int
	linkBase  int

	// Scratch space reused by every layout so a paint allocates nothing.
	wrapped     []string
	statusLines []string
	usageLines  []string

	// inputLayout is the message box's rows and scroll, and composer where the box
	// was painted, for the wheel.
	inputLayout components.TextareaLayout
	composer    gfx.Rect

	// pointer is the last pointer position in the window, for hover-only message
	// actions. answer is where the last answer, with its action row, was painted;
	// answerOver is whether the pointer was in it at the last paint or move.
	pointer    gfx.Point
	answer     gfx.Rect
	answerOver bool

	// textSelect is the conversation text selected with the pointer, and the drag
	// that is extending it. It may run through several messages and is never
	// saved. textFirst and textLast are the rows the last paint put on screen,
	// the ones a drag can reach; -1 when none was painted.
	textSelect          components.TextSelection
	textFirst, textLast int

	// The active message or history scrollbar's geometry and pointer drag.
	scrollbar aiScrollbarState

	// Dragging the panel's left edge or the message box's top edge.
	drag      aiDragKind
	dragFrom  float32 // where the pointer was when the box's edge was taken
	dragStart float32 // and how tall the box was
	boxHeight float32 // how tall it was painted

	// The model chip and its menu.
	modelMenu     bool
	modelProvider int                     // which provider of the menu is open
	modelList     components.MenuScroll   // where the list of that provider's models is scrolled to
	modelSubmenu  components.SubmenuState // which provider's model list is open
	modelHover    components.HoverIntent  // the provider row the pointer is resting on
	// modelTypeahead is the typed prefix that highlights a model by name.
	modelTypeahead components.MenuTypeahead
	modelsRect     gfx.Rect // where the list of models was painted, for the wheel
	window         gfx.Rect // the whole window, for the menu to be dismissed from
	chip           gfx.Rect
	approvalLay    aiApprovalLayout

	// The approval scope chip and the menu it opens. The scope itself belongs to
	// the conversation (ai.autoUpTo); this is only its view state.
	autoMenu   bool
	autoScroll components.MenuScroll
	scopeChip  gfx.Rect // where the chip was painted, for the menu to hang above

	// Reasoning effort chip and menu
	reasoningMenu   bool
	reasoningEffort string
	reasoningChip   gfx.Rect

	// mentions is what the @ before the caret can name, from the last layout.
	mentionBuf       [maxMentionSuggestions]mentionName
	mentions         []mentionName
	mentionIndex     int
	mentionText      string
	mentionSelection text.Range
	mentionDismissed bool

	// Strings made from the state once, not every paint.
	modelName, modelID, modelText string
	storeNotice                   string
	storeNoticeFor                error
	confirmFor                    *aiApproval
	confirmFormat, confirmText    string
	moreCount                     int
	moreFormat, moreText          string
}

// The panel stays available on every page, independently of terminal tabs.
func (state *State) aiPanelVisible() bool {
	return state.ai.open
}

// aiSectionReserve is the width the panel takes from the section beside it when
// no host is open, and zero while a workspace exists: the panel then belongs to
// the workspace's terminals (aiReserve).
func (state *State) aiSectionReserve(size gfx.Size) float32 {
	if state.terminalPage() {
		return 0
	}
	if reserve := state.aiReserve(0, size); reserve > 0 {
		// Replace the terminal divider with a gap between cards and an outer margin.
		return reserve - 1 + 2*sectionCardMargin
	}
	return 0
}

// aiReserve is the width a docked panel takes from the panes of a workspace, its
// divider included, or zero when the panel is closed or the window is too narrow
// to share (it is then a drawer over the panes).
func (state *State) aiReserve(workspaceID layoutcore.WorkspaceID, size gfx.Size) float32 {
	if !state.ai.open || size.Width < aiDockAt {
		return 0
	}
	panel := state.aiPanelExtent(size) + 1
	if state.aiSharedWidth(workspaceID, size)-panel < aiMinTerminal {
		return 0
	}
	return panel
}

// aiSharedWidth is the width the docked panel divides with the content beside it:
// the panes of a workspace, which may have a context panel of their own, or a
// section card, which sits under the fixed navigation rail. A workspace ID of
// zero names a section, as aiSectionReserve does.
func (state *State) aiSharedWidth(workspaceID layoutcore.WorkspaceID, size gfx.Size) float32 {
	base := state.baseWorkspacePixelBounds(size)
	if workspaceID == 0 {
		return size.Width - navigationWidth - state.toolbarDockSideReserve()
	}
	available := base.Width
	available -= state.independentMonitorReserve(workspaceID, size)
	if context, ok := state.deck.Context(workspaceID); ok && context.Open {
		available -= state.contextPanelWidth(base, context) + 1
	}
	return available
}

// aiPanelExtent is the width of the docked panel: what the user dragged it to,
// within the room the window has, or the default.
func (state *State) aiPanelExtent(size gfx.Size) float32 {
	if state.ai.panelWidth <= 0 {
		return min(aiPanelWidth, size.Width*0.42)
	}
	least, most := state.aiPanelLimits(size)
	return min(max(state.ai.panelWidth, least), most)
}

// aiPanelLimits is how narrow and how wide the panel may be: the content beside
// it keeps its minimum, and nothing else caps it. A wide window therefore gives a
// panel as wide as the user drags it, which long answers and code blocks need.
func (state *State) aiPanelLimits(size gfx.Size) (least, most float32) {
	workspaceID := state.deck.ActiveWorkspace()
	if !state.terminalPage() {
		workspaceID = 0
	}
	shared := state.aiSharedWidth(workspaceID, size) - aiMinTerminal - 1
	if size.Width < aiDockAt || shared < aiPanelMin {
		return aiPanelMin, max(size.Width*0.92, aiPanelMin)
	}
	return aiPanelMin, max(shared, aiPanelMin)
}

// aiPanelSync resizes the terminals when the panel's claim on the window
// changed. Opening, closing and the posted events that open it all go through
// here, so the rows and columns always follow what the panes are given.
func (state *State) aiPanelSync() {
	reserve := state.aiReserve(state.deck.ActiveWorkspace(), state.viewport)
	if reserve == state.ai.panel.fitted {
		return
	}
	state.ai.panel.fitted = reserve
	state.fitTerminals()
	state.fitSFTP(state.viewport)
}

// setAIPanelOpen shows or hides the panel.
func (state *State) setAIPanelOpen(open bool) {
	ai := &state.ai
	if ai.open == open {
		return
	}
	if open {
		id := state.deck.ActiveWorkspace()
		if state.monitorShown(id) && !state.monitorAndAICanCoexist(id, state.viewport) {
			state.closeMonitor()
		}
		// A stale hand-back from an earlier close must not fire after a reopen.
		ai.panel.focusBack = false
		// The model line reads what the database holds, so open it now rather
		// than during a paint.
		_, _ = state.aiStore()
	}
	ai.open = open
	if !open {
		ai.panel.targetOpen = false
		ai.panel.history = false
		ai.panel.modelMenu = false
		ai.panel.autoMenu = false
		ai.panel.scrollbar.dragging = false
		ai.panel.clearAITextSelection()
	}
	state.aiPanelSync()
}

// reconcileAIPanelWithMonitor keeps the assistant and the monitor from being
// left as two drawers at once: when both are open and the window cannot dock
// them side by side, the monitor keeps the space and the assistant closes, its
// conversation preserved, as showMonitor already decides for the reverse order.
// The window must never hold two drawers, and never a scope-less assistant
// drawer under the monitor's trapped one. Resize and a question the assistant
// raises on its own call this.
func (state *State) reconcileAIPanelWithMonitor(size gfx.Size) {
	if !state.ai.open {
		return
	}
	workspace := state.deck.ActiveWorkspace()
	if !state.monitorShown(workspace) || state.monitorAndAICanCoexist(workspace, size) {
		return
	}
	state.setAIPanelOpen(false)
}

// toggleAIPanel is the window-bar button, the shortcut and the palette entry:
// it only controls panel visibility, preserving the current page and tab.
func (state *State) toggleAIPanel() {
	if state.aiPanelVisible() {
		state.setAIPanelOpen(false)
		return
	}
	state.setAIPanelOpen(true)
}

// openAIPanelWith shows the panel and puts prompt in the message box without
// sending it, after whatever the user had already typed.
func (state *State) openAIPanelWith(prompt string) {
	ai := &state.ai
	if !state.aiPanelVisible() {
		state.toggleAIPanel()
	}
	ai.panel.history = false
	if existing := ai.input.String(); strings.TrimSpace(existing) != "" {
		ai.input.Insert("\n\n" + prompt)
		return
	}
	ai.input = text.Editor{}
	ai.input.Insert(prompt)
}

// aiPanelReset forgets the layout of the conversation shown.
func (state *State) aiPanelReset() {
	panel := &state.ai.panel
	clear(panel.rows)
	panel.rows = panel.rows[:0]
	panel.history = false
	panel.copied = ""
	panel.scrollbar.dragging = false
	panel.clearAITextSelection()
}

func (state *State) aiPanelClick(id gfx.ID) bool {
	// Modal controls (including AI rename) are dispatched by State.Click.
	if state.modal != modalNone || !inAIPanel(id) {
		return false
	}
	ai := &state.ai
	panel := &ai.panel
	switch {
	case id == hitAITarget:
		if ai.run == nil {
			panel.targetOpen = !panel.targetOpen
			panel.modelMenu = false
			panel.autoMenu = false
		}
	case id == hitAITargetDismiss:
		panel.targetOpen = false
	case id.Scope() == hitAITargets || id.Scope() == hitAITargetTabs:
		state.aiChooseTarget(id)
	case id == hitAIToggle:
		state.toggleAIPanel()
	case id == hitAIClose || id == hitAIDrawerDismiss:
		// Closing from inside the panel takes the keyboard with it; hand it back
		// to the terminal the panel covered.
		state.ai.panel.focusBack = true
		state.setAIPanelOpen(false)
	case id == hitAIAttach:
		state.aiPickAttachments()
	case id == hitAIUpload:
		state.aiPickUploads()
	case id.Scope() == hitAIAttachRemove:
		if index, ok := id.In(hitAIAttachRemove); ok {
			state.aiRemoveAttachment(index)
		}
	case id == hitAINew:
		state.aiNewConversation()
		if ai.run == nil {
			state.aiPanelReset()
		}
	case id == hitAIHistory:
		panel.history = !panel.history
		panel.clearAITextSelection()
		if panel.history {
			state.aiPanelLoadHistory()
		}
	case id == hitAIMode:
		state.dropEscalationApprovals()
		if ai.mode == agent.ModeAsk {
			state.setAIMode(agent.ModeAgent)
		} else {
			state.setAIMode(agent.ModeAsk)
		}
	case id == hitAISetup:
		state.openSettings()
		state.selectSettingsCategory(settingsAssistant)
		if ai.needLogin != "" {
			id := ai.needLogin
			ai.cfg.selected, ai.needLogin = id, ""
			state.aiStartCLILogin(id, false)
		}
	case id == hitAIModelChip:
		panel.targetOpen = false
		panel.autoMenu = false
		panel.reasoningMenu = false
		panel.modelMenu = !panel.modelMenu
		panel.modelTypeahead.Reset()
		panel.modelHover.Reset()
		if panel.modelMenu {
			panel.modelProvider, panel.modelList = state.aiMenuOpening()
			panel.modelSubmenu = panel.modelSubmenu.Open(hitAIModelProviders.At(panel.modelProvider))
		} else {
			panel.modelSubmenu = panel.modelSubmenu.Close()
		}
	case id.Scope() == hitAIModelProviders:
		if index, ok := id.In(hitAIModelProviders); ok {
			panel.modelProvider, panel.modelList = index, components.MenuScroll{BarID: hitAIModelBar}
			panel.modelSubmenu = panel.modelSubmenu.Open(id)
			panel.modelTypeahead.Reset()
		}
	case id == hitAIModelDismiss:
		panel.modelMenu = false
		panel.modelSubmenu = panel.modelSubmenu.Close()
	case id == hitAIModelSubDismiss:
		panel.modelSubmenu = panel.modelSubmenu.Close()
	case id == hitAIAutoScope:
		if !state.aiScopeLocked() {
			panel.targetOpen = false
			panel.modelMenu = false
			panel.reasoningMenu = false
			panel.autoMenu = !panel.autoMenu
			panel.autoScroll = components.MenuScroll{}
		}
	case id == hitAIAutoScopeDismiss:
		panel.autoMenu = false
	case id.Scope() == hitAIAutoScopeLevels:
		if index, ok := id.In(hitAIAutoScopeLevels); ok && index < len(state.aiScopes()) {
			state.aiSetAutoUpTo(state.aiScopes()[index].level)
			panel.autoMenu = false
		}
	case id == hitAIReasoningChip:
		panel.targetOpen = false
		panel.modelMenu = false
		panel.autoMenu = false
		panel.reasoningMenu = !panel.reasoningMenu
	case id == hitAIReasoningDismiss:
		panel.reasoningMenu = false
	case id.Scope() == hitAIReasoningMenu:
		if index, ok := id.In(hitAIReasoningMenu); ok {
			_, levels, okLevels := state.aiCurrentModelReasoning()
			if okLevels && index < len(levels) {
				panel.reasoningEffort = levels[index]
			}
			panel.reasoningMenu = false
		}
	case id.Scope() == hitAIModels:
		if index, ok := id.In(hitAIModels); ok {
			state.aiChooseModel(index)
		}
	case id == hitAISend:
		if ai.run != nil {
			state.aiStop()
		} else {
			state.aiPanelSend()
		}
	case id == hitAIApprove:
		state.aiAnswerApproval(true)
	case id == hitAIDecline:
		state.aiAnswerApproval(false)
	case id == hitAIToEnd:
		ai.scroll.ScrollToEnd()
	case id.Scope() == hitAIExamples:
		_, _, examples := state.aiEmptyTexts()
		index, _ := id.In(hitAIExamples)
		ai.input = text.Editor{}
		ai.input.Insert(examples[index])
	case id.Scope() == hitAIMentions:
		if index, _ := id.In(hitAIMentions); index < len(panel.mentions) {
			state.acceptMention(panel.mentions[index].name)
		}
	case id.Scope() == hitAIHistoryOpen:
		if index, _ := id.In(hitAIHistoryOpen); index < len(ai.conversations) && ai.run == nil {
			state.aiPanelReset()
			state.aiOpenConversation(ai.conversations[index].ID)
		}
	case id.Scope() == hitAIHistoryRename:
		if index, ok := id.In(hitAIHistoryRename); ok && index < len(ai.conversations) {
			state.openAIRename(index)
		}
	case id.Scope() == hitAIHistoryDelete:
		if index, _ := id.In(hitAIHistoryDelete); index < len(ai.conversations) {
			state.aiPanelDeleteConversation(index)
		}
	case id.Scope() == hitAIRows:
		if index, _ := id.In(hitAIRows); index < len(panel.rows) {
			panel.rows[index].expanded = !panel.rows[index].expanded
		}
	case id.Scope() == hitAILinks:
		if index, ok := id.In(hitAILinks); ok {
			state.aiOpenLink(index)
		}
	case id == hitAICopyAnswer:
		state.aiCopyAnswer()
	case id.Scope() == hitAICopies:
		if index, _ := id.In(hitAICopies); index < panel.copyCount {
			if err := state.options.SetClipboardText(panel.copyTargets[index]); err != nil {
				ai.notice = err.Error()
			} else {
				panel.copied, panel.copiedAt = panel.copyTargets[index], state.now()
			}
		}
	}
	return true
}

// aiCopyAnswer copies the last assistant answer as Markdown.
func (state *State) aiCopyAnswer() {
	ai := &state.ai
	for index := len(ai.items) - 1; index >= 0; index-- {
		if ai.items[index].kind != aiAssistant {
			continue
		}
		text := ai.items[index].text
		if err := state.options.SetClipboardText(text); err != nil {
			ai.notice = err.Error()
			return
		}
		ai.panel.copied, ai.panel.copiedAt = text, state.now()
		return
	}
}

// aiPanelSend sends the message box; the model line is read from the settings,
// so a send without a model only tells the user how to get one.
func (state *State) aiPanelSend() {
	state.aiSend()
}

// aiPanelLoadHistory reads the saved conversations for the history list.
func (state *State) aiPanelLoadHistory() {
	ai := &state.ai
	panel := &ai.panel
	panel.historyTop = 0
	st, err := state.aiStore()
	if err != nil || st == nil {
		ai.conversations, panel.historyDates = nil, panel.historyDates[:0]
		return
	}
	list, err := st.Conversations("", aiMaxHistory)
	if err != nil {
		ai.notice = err.Error()
		return
	}
	ai.conversations = list
	panel.historyDates = panel.historyDates[:0]
	for _, conversation := range list {
		panel.historyDates = append(panel.historyDates, conversation.Updated.Local().Format("2006-01-02 15:04"))
	}
}

func (state *State) aiPanelDeleteConversation(index int) {
	ai := &state.ai
	st, err := state.aiStore()
	if err != nil || st == nil {
		return
	}
	id := ai.conversations[index].ID
	if err := st.DeleteConversation(id); err != nil {
		ai.notice = err.Error()
		return
	}
	if ai.conv == id && ai.run == nil {
		state.aiNewConversation()
		state.aiPanelReset()
		ai.panel.history = true
	}
	state.aiPanelLoadHistory()
}

// aiTrackPointer follows the pointer for the hover-only actions under the last
// answer. Nothing under the pointer is a hit while it rests on the answer's text,
// so the runtime would not repaint when it enters or leaves; this reports whether
// it did, and the caller asks for the repaint.
func (state *State) aiTrackPointer(event gfx.PointerEvent) (repaint bool) {
	panel := &state.ai.panel
	switch event.Kind {
	case gfx.PointerDown, gfx.PointerMove:
		panel.pointer = gfx.Point{X: event.X, Y: event.Y}
	case gfx.PointerExit:
		panel.pointer = gfx.Point{}
	default:
		return false
	}
	over := pointInRect(panel.pointer.X, panel.pointer.Y, panel.answer)
	changed := over != panel.answerOver
	panel.answerOver = over
	if event.Kind == gfx.PointerDown && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone) &&
		panel.hasAITextSelection() {
		if _, _, onText := aiTextTarget(event.Target); !onText {
			panel.clearAITextSelection()
			changed = true
		}
	}
	return changed
}

// aiTextTarget resolves a selectable-text hit to its conversation row and block.
func aiTextTarget(id gfx.ID) (row int, part aiTextPart, ok bool) {
	switch id.Scope() {
	case hitAIAnswerText:
		row, ok = id.In(hitAIAnswerText)
		return row, aiTextPrimary, ok
	case hitAIToolOutputText:
		row, ok = id.In(hitAIToolOutputText)
		return row, aiTextOutput, ok
	default:
		return 0, aiTextPrimary, false
	}
}

// aiReleaseMessageScroll disengages the live edge without moving the reader.
// Restoring the offset with AutoScroll temporarily off prevents ScrollTo from
// immediately taking hold of the edge again.
func aiReleaseMessageScroll(scroll *components.MessageScrollState) {
	offset, autoScroll := scroll.Offset(), scroll.AutoScroll
	scroll.AutoScroll = false
	scroll.ScrollToStart()
	scroll.ScrollTo(offset)
	scroll.AutoScroll = autoScroll
}

// aiPanelScrollbarPointer drags the active message or history thumb and pages
// when its track is pressed. Painting owns the geometry; the panel owns the offset.
func (state *State) aiPanelScrollbarPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	panel := &state.ai.panel
	bar := &panel.scrollbar
	// A window that deactivates mid-drag (Cmd-Tab) never delivers the PointerUp.
	// Cancel on a press elsewhere and let that press through, so the panel does
	// not answer every later click with the stale gesture.
	if event.Kind == gfx.PointerDown && bar.dragging &&
		event.Target != hitAIMessageBar && event.Target != hitAIHistoryBar {
		bar.dragging = false
		return gfx.EventResult{}, false
	}
	history := event.Target == hitAIHistoryBar
	if event.Kind == gfx.PointerDown && (history || event.Target == hitAIMessageBar) {
		primary := event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone
		geometry := bar.geometry
		if !primary || history != panel.listing || geometry.MaxOffset <= 0 {
			return gfx.EventResult{}, false
		}
		page := panel.list.Height * 0.9
		switch {
		case event.Y < geometry.Thumb.Y:
			return state.aiPanelScrollBy(-page), true
		case event.Y >= geometry.Thumb.Y+geometry.Thumb.Height:
			return state.aiPanelScrollBy(page), true
		}
		result := gfx.EventResult{Handled: true, Repaint: true, Focus: event.Target}
		if geometry.Track.Height <= geometry.Thumb.Height {
			return result, true
		}
		if !history {
			aiReleaseMessageScroll(&state.ai.scroll)
		}
		bar.dragging = true
		bar.history = history
		bar.grab = event.Y - geometry.Thumb.Y
		bar.dragGeometry = geometry
		return result, true
	}
	if !bar.dragging {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerExit {
		bar.dragging = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if bar.history != panel.listing || !state.aiPanelVisible() {
		bar.dragging = false
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind == gfx.PointerUp {
		// Moves already applied the gesture. Recomputing on Up would jump if the
		// content extent changed while the pointer stayed still.
		bar.dragging = false
		return gfx.EventResult{Handled: true}, true
	}
	if event.Kind == gfx.PointerMove && !event.Captured {
		return gfx.EventResult{}, false
	}
	if event.Kind != gfx.PointerMove {
		return gfx.EventResult{Handled: true}, true
	}
	offset := bar.dragGeometry.OffsetForThumb(event.Y - bar.grab)
	return state.aiPanelScrollTo(offset), true
}

// aiPanelPointer moves the focus to the message box when an example is released,
// and follows a drag of the panel's or the box's edge.
//
// It begins, extends and ends a selection of one conversation text block. The
// text hit is a focusable group outside the Tab order; links and copy buttons
// are declared after it and still win the press.
func (state *State) aiPanelPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	panel := &state.ai.panel
	if result, handled := state.aiPanelScrollbarPointer(event); handled {
		return result, true
	}
	primary := event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone
	if event.Kind == gfx.PointerDown && primary {
		if _, _, ok := aiTextTarget(event.Target); ok {
			if result := panel.textSelect.Pointer(event, panel.textPointAt); result.Handled {
				return gfx.EventResult{Handled: true, Repaint: result.Changed, Focus: event.Target}, true
			}
		}
	} else if panel.textSelect.Dragging() {
		// A hovered move does not belong to the press and does not extend the
		// selection, matching the terminal's drag; leaving the window ends the
		// drag and keeps what was selected.
		if result := panel.textSelect.Pointer(event, panel.textPointAt); result.Handled {
			return gfx.EventResult{Handled: true, Repaint: result.Changed}, true
		}
	}
	if event.Kind == gfx.PointerMove {
		if index, ok := event.Target.In(hitAIMentions); ok && index < len(panel.mentions) && panel.mentionIndex != index {
			panel.mentionIndex = index
			return gfx.EventResult{Repaint: true}, true
		}
	}
	if event.Kind == gfx.PointerUp && (event.Target.Scope() == hitAIExamples || event.Target.Scope() == hitAIMentions) && event.Button == gfx.PointerButtonPrimary {
		// The click that follows fills the prompt in; the box takes the focus so the
		// user can edit it or press Enter.
		return gfx.EventResult{Focus: hitAIInput, Repaint: true}, true
	}
	if event.Kind == gfx.PointerDown && (event.Target == hitAIDivider || event.Target == hitAIComposerGrip) {
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{}, true
		}
		panel.drag = aiDragWidth
		panel.dragFrom, panel.dragStart = event.X, panel.rect.Width
		if event.Target == hitAIComposerGrip {
			panel.drag, panel.dragFrom, panel.dragStart = aiDragHeight, event.Y, panel.boxHeight
		}
		return state.aiDrag(event), true
	}
	if panel.drag == aiDragNone {
		return gfx.EventResult{}, false
	}
	switch event.Kind {
	case gfx.PointerMove:
		if event.Captured {
			return state.aiDrag(event), true
		}
	case gfx.PointerUp:
		result := state.aiDrag(event)
		panel.drag = aiDragNone
		state.save()
		result.Handled = true
		return result, true
	case gfx.PointerExit:
		panel.drag = aiDragNone
		return gfx.EventResult{Handled: true}, true
	}
	return gfx.EventResult{Handled: true}, true
}

// aiDrag follows the pointer while the panel's left edge or the message box's top
// edge is being dragged.
func (state *State) aiDrag(event gfx.PointerEvent) gfx.EventResult {
	ai, panel := &state.ai, &state.ai.panel
	switch panel.drag {
	case aiDragWidth:
		least, most := state.aiPanelLimits(event.Viewport)
		ai.panelWidth = min(max(panel.dragStart+panel.dragFrom-event.X, least), most)
		state.aiPanelSync()
	case aiDragHeight:
		ai.composerHeight = max(panel.dragStart+panel.dragFrom-event.Y, 0)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

// aiPanelScroll scrolls the message list, or the history list. A wheel over the
// rest of the panel is swallowed: it must not scroll the terminal behind it.
func (state *State) aiPanelScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	panel := &state.ai.panel
	if panel.autoMenu {
		return gfx.EventResult{Handled: true, Repaint: panel.autoScroll.Wheel(event)}, true
	}
	if panel.modelMenu && pointInRect(event.X, event.Y, panel.modelsRect) {
		return gfx.EventResult{Handled: true, Repaint: panel.modelList.Wheel(event)}, true
	}
	if panel.rect.Width <= 0 || !pointInRect(event.X, event.Y, panel.rect) {
		return gfx.EventResult{}, false
	}
	if pointInRect(event.X, event.Y, panel.composer) {
		panel.inputLayout.ScrollBy(event.DeltaY)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if !pointInRect(event.X, event.Y, panel.list) {
		return gfx.EventResult{Handled: true}, true
	}
	return state.aiPanelScrollBy(event.StepY(24)), true
}

// aiPanelScrollTo moves the active list to an absolute offset.
func (state *State) aiPanelScrollTo(offset float32) gfx.EventResult {
	ai := &state.ai
	panel := &ai.panel
	if panel.listing {
		before := panel.historyTop
		panel.historyTop = min(max(offset, 0), panel.maxTop)
		return gfx.EventResult{Handled: true, Repaint: panel.historyTop != before}
	}
	before := ai.scroll.Offset()
	ai.scroll.ScrollTo(offset)
	return gfx.EventResult{Handled: true, Repaint: ai.scroll.Offset() != before}
}

// aiPanelScrollbarKey gives the focused slider the same keyboard and
// accessibility-increment behavior as a scroll area.
func (state *State) aiPanelScrollbarKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	panel := &state.ai.panel
	history := event.Target == hitAIHistoryBar
	if (!history && event.Target != hitAIMessageBar) || history != panel.listing {
		return gfx.EventResult{}, false
	}
	offset, maximum := state.ai.scroll.Offset(), panel.maxScroll
	if history {
		offset, maximum = panel.historyTop, panel.maxTop
	}
	area := components.BeginScrollArea(nil, state.Theme(), panel.list, gfx.Size{Width: panel.list.Width, Height: panel.list.Height + maximum}).Offset(0, offset)
	_, next, ok := area.ScrollForKey(0, offset, event.Key, 24)
	if !ok {
		return gfx.EventResult{}, false
	}
	return state.aiPanelScrollTo(next), true
}

// aiPanelScrollBy moves the list by step pixels. Reaching the end of the
// conversation makes it follow new messages again; leaving it stops that.
func (state *State) aiPanelScrollBy(step float32) gfx.EventResult {
	ai := &state.ai
	panel := &ai.panel
	if panel.listing {
		before := panel.historyTop
		panel.historyTop = min(max(before+step, 0), panel.maxTop)
		return gfx.EventResult{Handled: true, Repaint: panel.historyTop != before}
	}
	before := ai.scroll.Offset()
	ai.scroll.ScrollBy(step)
	return gfx.EventResult{Handled: true, Repaint: ai.scroll.Offset() != before}
}

// aiAutoScopeKey moves through and closes the approval chip's menu. A scope that
// is fixed for the running request is not opened again: its rows would report a
// choice the request does not take.
func (state *State) aiAutoScopeKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	panel := &state.ai.panel
	onChip := event.Target == hitAIAutoScope
	onRow := event.Target.Scope() == hitAIAutoScopeLevels
	if !panel.autoMenu {
		if !onChip || event.Repeat || (event.Key != gfx.KeyDown && event.Key != gfx.KeyUp) || state.aiScopeLocked() {
			return gfx.EventResult{}, false
		}
		panel.targetOpen = false
		panel.modelMenu = false
		panel.autoMenu = true
		panel.autoScroll = components.MenuScroll{}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitAIAutoScopeLevels.At(0), FocusVisible: true}, true
	}
	if !onChip && !onRow {
		return gfx.EventResult{}, false
	}
	backToChip := gfx.EventResult{Handled: true, Repaint: true, Focus: hitAIAutoScope, FocusVisible: true}
	switch event.Key {
	case gfx.KeyEscape:
		panel.autoMenu = false
		return backToChip, true
	case gfx.KeyEnter, gfx.KeySpace:
		if event.Repeat || !onRow {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if index, ok := event.Target.In(hitAIAutoScopeLevels); ok && index < len(state.aiScopes()) {
			state.aiSetAutoUpTo(state.aiScopes()[index].level)
			panel.autoMenu = false
		}
		return backToChip, true
	case gfx.KeyUp, gfx.KeyDown, gfx.KeyHome, gfx.KeyEnd, gfx.KeyPageUp, gfx.KeyPageDown:
		if event.Repeat {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if id, ok := panel.autoScroll.Navigate(event.Key, event.Target, true); ok {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
		}
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{}, false
}

func (state *State) aiPanelKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNone || !inAIPanel(event.Target) || event.Target == hitAIToggle {
		return gfx.EventResult{}, false
	}
	ai := &state.ai
	panel := &ai.panel
	if event.Target.Scope() == hitAIExamples && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
		return gfx.EventResult{Focus: hitAIInput, Repaint: true}, true
	}
	if result, handled := state.aiAutoScopeKey(event); handled {
		return result, true
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if result, handled := state.aiPanelScrollbarKey(event); handled {
		return result, true
	}
	if event.Target == hitAITarget && ai.run == nil && !panel.targetOpen && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		panel.targetOpen = true
		panel.modelMenu = false
		return handled, true
	}
	// Right opens the provider's models and Left returns to the provider row,
	// as a submenu moves between levels in Base UI.
	if command, ok := components.SubmenuCommandForKey(event.Key); ok && !event.Repeat {
		if _, providerRow := event.Target.In(hitAIModelProviders); providerRow && command == components.SubmenuCommandOpen {
			if index, _ := event.Target.In(hitAIModelProviders); index < len(state.aiMenuProviders()) {
				panel.modelProvider = index
				panel.modelList = components.MenuScroll{BarID: hitAIModelBar}
				panel.modelSubmenu = panel.modelSubmenu.Open(event.Target)
				panel.modelTypeahead.Reset()
				providers := state.aiMenuProviders()
				if len(providers[index].models) > 0 {
					return gfx.EventResult{Handled: true, Repaint: true, Focus: hitAIModels.At(0), FocusVisible: true}, true
				}
				return handled, true
			}
		}
		if command == components.SubmenuCommandClose && panel.modelSubmenu.IsAnyOpen() {
			trigger := panel.modelSubmenu.Trigger()
			panel.modelSubmenu = panel.modelSubmenu.Close()
			return gfx.EventResult{Handled: true, Repaint: true, Focus: trigger, FocusVisible: true}, true
		}
	}
	if event.Target == hitAIInput {
		if _, composing := ai.input.Composition(); composing {
			return gfx.EventResult{}, false
		}
	}
	if state.aiMentionKey(event) {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitAIInput}, true
	}
	if event.Target == hitAIInput {
		// Up, Down, Home, End and the page keys move by the rows the box wrapped to.
		if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
			value := ai.input.String()
			to := panel.inputLayout.Move(value, aiByteOffset(value, ai.input.Caret()), movement)
			ai.input.MoveTo(utf8.RuneCountInString(value[:to]), extend)
			return handled, true
		}
	}
	switch event.Key {
	case gfx.KeyEscape:
		if event.Repeat {
			return handled, true
		}
		if len(ai.approvals) > 0 {
			state.aiAnswerApproval(false)
			return handled, true
		}
		if panel.history {
			panel.history = false
			return handled, true
		}
		if _, _, onText := aiTextTarget(event.Target); onText && panel.hasAITextSelection() {
			// Approvals, history and menus have already had their chance above;
			// at this point Escape belongs to the selected conversation text.
			panel.clearAITextSelection()
			return handled, true
		}
		if _, composing := ai.input.Composition(); composing && event.Target == hitAIInput {
			return gfx.EventResult{}, false // the input method owns Escape
		}
		if event.Target == hitAIInput {
			// Back to the terminal the panel is about.
			return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
		}
	case gfx.KeyEnter:
		switch event.Target {
		case hitAIInput:
			if event.Repeat {
				return handled, true
			}
			if event.Modifiers&gfx.ModifierShift != 0 {
				ai.input.Insert("\n")
				return handled, true
			}
			state.aiPanelEnter()
			return handled, true
		case hitAIConfirm:
			if !event.Repeat {
				state.aiAnswerApproval(true)
			}
			return handled, true
		}
	case gfx.KeyPageUp, gfx.KeyPageDown:
		if event.Target == hitAIInput || panel.list.Height <= 0 {
			return gfx.EventResult{}, false
		}
		step := panel.list.Height * 0.9
		if event.Key == gfx.KeyPageUp {
			step = -step
		}
		return state.aiPanelScrollBy(step), true
	}
	return gfx.EventResult{}, false
}

// aiPanelEnter is Enter in the message box. With a question waiting for an
// answer and nothing typed, it answers yes (when the word is in, if one was
// asked for); otherwise it sends.
func (state *State) aiPanelEnter() {
	ai := &state.ai
	if len(ai.approvals) > 0 && strings.TrimSpace(ai.input.String()) == "" {
		state.aiAnswerApproval(true)
		return
	}
	state.aiPanelSend()
}

// aiPanelShortcut handles the chords of the message box that the native text
// system must not see first. Ctrl+J (Cmd+J on macOS) starts a new line too, for
// keyboards and input methods where Shift+Enter is taken; Shift+Enter itself
// reaches the native editor as app.TextInsertLineBreak (see aiPanelText). On
// selectable conversation text it owns the copy chord, so a selection never
// falls through to the terminal's copy.
func (state *State) aiPanelShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if _, _, onText := aiTextTarget(event.Target); onText {
		if chord, ok := keymap.FromEvent(event); ok && state.shortcuts.actionFor(chord) == shortcutCopy {
			if !event.Repeat {
				state.aiCopyTextSelection()
			}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	if event.Target != hitAIInput || event.Text != "j" || event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 ||
		event.Modifiers&^(gfx.ModifierControl|gfx.ModifierMeta) != 0 {
		return gfx.EventResult{}, false
	}
	if !event.Repeat {
		state.ai.input.Insert("\n")
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// aiCopyTextSelection writes the selected conversation text to the clipboard.
// With no selection it does nothing, and in particular never copies a whole row.
func (state *State) aiCopyTextSelection() {
	panel := &state.ai.panel
	text, ok := panel.aiTextSelectionText()
	if !ok {
		return
	}
	if err := state.options.SetClipboardText(text); err != nil {
		state.ai.notice = err.Error()
	}
}

// aiPanelEditor serves the single-line fields of the panel: the word that a
// critical action asks to be typed. The message box is a multi-line editor and
// is served by AIPanelInput.
func (state *State) aiPanelEditor(hit gfx.ID) *text.Editor {
	if hit == hitAIConfirm && len(state.ai.approvals) > 0 && state.ai.approvals[0].request.NeedsText {
		return &state.ai.confirm
	}
	return nil
}

// AIPanelInput resolves the native editor of the assistant's message box. It
// edits the same text.Editor the panel paints, but takes Enter itself: Enter
// accepts the highlighted @ suggestion, or sends the message when no suggestion
// is open. Shift+Enter (app.TextInsertLineBreak) still inserts a line break.
func (state *State) AIPanelInput(context app.TextInputContext) *app.TextInput {
	if context.ID != hitAIInput || state.ai.panel.rect.Width <= 0 {
		return nil
	}
	panel := &state.ai.panel
	panel.input.Editor = nil
	panel.input.State = app.TextStateOf(&state.ai.input)
	if panel.input.OnEvent == nil {
		panel.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
			return state.aiPanelText(event), nil
		}
	}
	return &panel.input
}

func (state *State) aiPanelText(event app.TextEvent) app.TextState {
	ai := &state.ai
	if event.Kind == app.TextPasteNoText {
		// Nothing to insert: a picture on the clipboard is attached instead.
		state.aiPasteImage()
		result := app.TextStateOf(&ai.input)
		result.Redraw = true // the chips changed, not the editor
		return result
	}
	if (event.Kind == app.TextPaste || event.Kind == app.TextInsert) && isLongAIText(event.Text) {
		// Long text is attached as a file rather than dumped into the message box,
		// keeping the composer responsive and preventing renderer overflow.
		state.aiPasteText(event.Text)
		result := app.TextStateOf(&ai.input)
		result.Redraw = true
		return result
	}
	_, composing := ai.input.Composition()
	if event.Kind == app.TextCommandEvent && !composing && (event.Command == app.TextInsertNewline || event.Command == app.TextInsertTab) {
		if state.aiAcceptSelectedMention() {
			result := app.TextStateOf(&ai.input)
			result.Redraw = true
			return result
		}
		if event.Command == app.TextInsertNewline {
			state.aiPanelEnter()
			result := app.TextStateOf(&ai.input)
			result.Redraw = true // the conversation changed, not only the editor
			return result
		}
	}
	event.Apply(&ai.input)
	return app.TextStateOf(&ai.input)
}

// aiPanelSelection is the quoted text that "ask about the selection" puts in
// the message box.
func (state *State) aiAskAboutSelection(session *Session) {
	if session.term == nil || !session.term.selection.selected {
		return
	}
	selection := session.term.selection
	quoted := strings.TrimSpace(selection.text(session.term.screen))
	if quoted == "" {
		return
	}
	const limit = 4000
	if len(quoted) > limit {
		cut := limit
		for cut > 0 && quoted[cut]&0xc0 == 0x80 {
			cut--
		}
		quoted = quoted[:cut] + "\n…"
	}
	state.setAIMode(agent.ModeAsk)
	state.openAIPanelWith(state.aiReferencePrompt(session, state.messages.AIPanel.PromptSelectionFn+"\n```\n"+quoted+"\n```\n"))
}

// aiExplainOutput puts the question about the terminal's recent output in the
// message box; Ask mode sends the screen's text along with it.
func (state *State) aiExplainOutput(session *Session) {
	state.setAIMode(agent.ModeAsk)
	state.openAIPanelWith(state.aiReferencePrompt(session, state.messages.AIPanel.PromptExplain))
}

// A terminal-menu shortcut references its source without changing a locked
// target. In particular, explaining B must not accidentally attach only A.
func (state *State) aiReferencePrompt(session *Session, prompt string) string {
	if state.aiTarget().ID != sessionHandle(session.ID) {
		for _, name := range state.mentionNames(nil) {
			if name.session == session.ID {
				return "@" + name.name + " " + prompt
			}
		}
	}
	return prompt
}

// aiOpenLink opens the link behind hit number index of the last paint.
func (state *State) aiOpenLink(index int) {
	panel := &state.ai.panel
	for n := panel.linkRows - 1; n >= 0; n-- {
		entry := panel.linkTable[n]
		if index < entry.base || entry.row >= len(panel.rows) {
			continue
		}
		if dest := panel.rows[entry.row].md.Link(index - entry.base); dest != "" {
			state.openExternal(dest)
		}
		return
	}
}
