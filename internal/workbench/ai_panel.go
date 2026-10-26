package workbench

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
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
	hitAIToggle          = tree.ID(aiPanelHits, "ai-toggle")
	hitAIClose           = tree.ID(aiPanelHits, "ai-close")
	hitAINew             = tree.ID(aiPanelHits, "ai-new")
	hitAIHistory         = tree.ID(aiPanelHits, "ai-history")
	hitAIMode            = tree.ID(aiPanelHits, "ai-mode")
	hitAISetup           = tree.ID(aiPanelHits, "ai-setup")
	hitAISend            = tree.ID(aiPanelHits, "ai-send")
	hitAIInput           = tree.ID(aiPanelHits, "ai-input")
	hitAIConfirm         = tree.ID(aiPanelHits, "ai-confirm")
	hitAIApprove         = tree.ID(aiPanelHits, "ai-approve")
	hitAIDecline         = tree.ID(aiPanelHits, "ai-decline")
	hitAIToEnd           = tree.ID(aiPanelHits, "ai-to-end")
	hitAIDrawerDismiss   = tree.ID(aiPanelHits, "ai-drawer-dismiss")
	hitAIModelChip       = tree.ID(aiPanelHits, "ai-model-chip")
	hitAIDivider         = tree.ID(aiPanelHits, "ai-divider")
	hitAIComposerGrip    = tree.ID(aiPanelHits, "ai-composer-grip")
	hitAIModelDismiss    = tree.ID(aiPanelHits, "ai-model-dismiss")
	hitAIModelSubDismiss = tree.ID(aiPanelHits, "ai-model-submenu-dismiss")
	hitAIModelList       = tree.ID(aiPanelHits, "ai-model-list") // names the models menu for its entrance and exit; it is not a control
	hitAIModelBar        = tree.ID(aiPanelHits, "ai-model-scroll-bar")
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
	hitAIExamples       = tree.Sub(aiPanelHits, tree.Name("ai-examples"))       // the example prompts
	hitAIHistoryOpen    = tree.Sub(aiPanelHits, tree.Name("ai-history-open"))   // saved conversations, to open
	hitAIHistoryDelete  = tree.Sub(aiPanelHits, tree.Name("ai-history-delete")) // and to delete
	hitAIHistoryRename  = tree.Sub(aiPanelHits, tree.Name("ai-history-rename")) // and to rename
	hitAIRenameDismiss  = tree.ID(aiPanelHits, "ai-rename-dismiss")
	hitAIRenameClose    = tree.ID(aiPanelHits, "ai-rename-close")
	hitAIRenameCancel   = tree.ID(aiPanelHits, "ai-rename-cancel")
	hitAIRenameSubmit   = tree.ID(aiPanelHits, "ai-rename-submit")
	hitAIRenameField    = tree.ID(aiPanelHits, "ai-rename-field")
	hitAIRows           = tree.Sub(aiPanelHits, tree.Name("ai-rows"))            // collapsible rows of the conversation
	hitAICopies         = tree.Sub(aiPanelHits, tree.Name("ai-copies"))          // copy buttons of code blocks
	hitAICopyAnswer     = tree.ID(aiPanelHits, "ai-copy-answer")                 // copy the last answer
	hitAILinks          = tree.Sub(aiPanelHits, tree.Name("ai-links"))           // links in the assistant's answers
	hitAIMentions       = tree.Sub(aiPanelHits, tree.Name("ai-mentions"))        // sessions an @ can name
	hitAIModels         = tree.Sub(aiPanelHits, tree.Name("ai-models"))          // the models of the provider the chip menu has open
	hitAIModelProviders = tree.Sub(aiPanelHits, tree.Name("ai-model-providers")) // the providers the chip menu lists
)

// The terminal menu's two assistant items.
var (
	hitTerminalMenuAsk     = tree.ID(terminalMenuHits, "ask")
	hitTerminalMenuExplain = tree.ID(terminalMenuHits, "explain")
)

const (
	aiPanelWidth  = float32(380)            // the preferred width of the docked panel
	aiPanelMin    = float32(300)            // the least it can be dragged to
	aiPanelMax    = float32(760)            // and the most
	aiDockAt      = float32(900)            // narrower windows get a drawer instead
	aiCopiedFor   = 1500 * time.Millisecond // how long a copy button shows its check
	aiMinTerminal = float32(360)            // the panes keep at least this much beside the panel
)

func inAIPanel(id ui.ID) bool { return tree.Within(id.Scope(), aiPanelHits) }

type aiDragKind uint8

const (
	aiDragNone aiDragKind = iota
	aiDragWidth
	aiDragHeight
)

// aiLinkRow is a message whose links were painted: its first hit number.
type aiLinkRow struct{ base, row int }

// aiPanel is what the panel keeps beyond the conversation itself: layout caches,
// the geometry of the last paint and small view state. It is all derived or
// cosmetic; nothing here is saved.
type aiPanel struct {
	rows []aiRowView

	// history shows the saved conversations in place of the messages.
	history      bool
	historyDates []string
	historyTop   float32

	// Geometry of the last paint, for the wheel and the text input.
	rect      ui.Rect // the whole panel; empty when it was not painted
	list      ui.Rect // the message list (or the history list)
	listing   bool    // list is the history list
	maxScroll float32 // largest scroll offset of the message list
	maxTop    float32 // and of the history list

	// fitted is the width the terminals were last sized for.
	fitted float32

	input app.TextInput // the native editor of the message box, reused

	copyTargets [aiMaxCopyBlocks]string
	// rowSlots and copies are where flex put the rows on screen and, in the row
	// being painted, the copy buttons of its code blocks. They are only valid
	// during the paint that filled them and are emptied at the start of the next.
	rowSlots  []flex.Box
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
	wrapped []string

	// inputLayout is the message box's rows and scroll, and composer where the box
	// was painted, for the wheel.
	inputLayout components.TextareaLayout
	composer    ui.Rect

	// pointer is the last pointer position in the window, for hover-only message
	// actions. answer is where the last answer, with its action row, was painted;
	// answerOver is whether the pointer was in it at the last paint or move.
	pointer    ui.Point
	answer     ui.Rect
	answerOver bool

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
	modelsRect     ui.Rect // where the list of models was painted, for the wheel
	window         ui.Rect // the whole window, for the menu to be dismissed from
	chip           ui.Rect
	approvalLay    aiApprovalLayout

	// mentions is what the @ before the caret can name, from the last layout.
	mentionBuf [maxMentionSuggestions]mentionName
	mentions   []mentionName

	// Strings made from the state once, not every paint.
	modelName, modelID, modelText string
	storeNotice                   string
	storeNoticeFor                error
	confirmFor                    *aiApproval
	confirmFormat, confirmText    string
	moreCount                     int
	moreFormat, moreText          string
}

// aiPanelVisible reports whether the panel is on screen: it is open and either a
// terminal workspace is showing, or there is none at all and it sits beside
// whatever section the window shows.
func (state *State) aiPanelVisible() bool {
	return state.ai.open && (state.deck.ActiveWorkspace() == 0 || state.activeSection == sectionTerminal)
}

// aiSectionReserve is the width the panel takes from the section beside it when
// no host is open, and zero while a workspace exists: the panel then belongs to
// the workspace's terminals (aiReserve).
func (state *State) aiSectionReserve(size ui.Size) float32 {
	if state.deck.ActiveWorkspace() != 0 {
		return 0
	}
	return state.aiReserve(0, size)
}

// aiReserve is the width a docked panel takes from the panes of a workspace, its
// divider included, or zero when the panel is closed or the window is too narrow
// to share (it is then a drawer over the panes).
func (state *State) aiReserve(workspaceID layoutcore.WorkspaceID, size ui.Size) float32 {
	if !state.ai.open || size.Width < aiDockAt {
		return 0
	}
	base := state.baseWorkspacePixelBounds(size)
	available := base.Width
	if context, ok := state.deck.Context(workspaceID); ok && context.Open {
		available -= state.contextPanelExtent(base, context.Width) + 1
	}
	panel := state.aiPanelExtent(size) + 1
	if available-panel < aiMinTerminal {
		return 0
	}
	return panel
}

// aiPanelExtent is the width of the docked panel: what the user dragged it to,
// within the room the window has, or the default.
func (state *State) aiPanelExtent(size ui.Size) float32 {
	if state.ai.panelWidth <= 0 {
		return min(aiPanelWidth, size.Width*0.42)
	}
	least, most := state.aiPanelLimits(size)
	return min(max(state.ai.panelWidth, least), most)
}

// aiPanelLimits is how narrow and how wide the panel may be: the panes keep their
// minimum beside it.
func (state *State) aiPanelLimits(size ui.Size) (least, most float32) {
	base := state.baseWorkspacePixelBounds(size)
	available := base.Width
	if context, ok := state.deck.Context(state.deck.ActiveWorkspace()); ok && context.Open {
		available -= state.contextPanelExtent(base, context.Width) + 1
	}
	most = min(aiPanelMax, size.Width*0.6, available-aiMinTerminal-1)
	return min(aiPanelMin, most), max(most, aiPanelMin)
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
	if open {
		// The model line reads what the database holds, so open it now rather
		// than during a paint.
		_, _ = state.aiStore()
	}
	ai.open = open
	if !open {
		ai.panel.history = false
		ai.panel.modelMenu = false
	}
	state.aiPanelSync()
}

// toggleAIPanel is the window-bar button, the shortcut and the palette entry:
// it closes a panel that is showing, and otherwise brings it up, from any
// section, over the active terminal workspace, or beside the section when there
// is no workspace.
func (state *State) toggleAIPanel() {
	if state.aiPanelVisible() {
		state.setAIPanelOpen(false)
		return
	}
	if workspace := state.deck.ActiveWorkspace(); workspace != 0 && state.activeSection != sectionTerminal && state.activateWorkspace(workspace) {
		state.save()
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
}

func (state *State) aiPanelClick(id ui.ID) bool {
	if !inAIPanel(id) {
		return false
	}
	ai := &state.ai
	panel := &ai.panel
	switch {
	case id == hitAIToggle:
		state.toggleAIPanel()
	case id == hitAIClose || id == hitAIDrawerDismiss:
		state.setAIPanelOpen(false)
	case id == hitAINew:
		state.aiNewConversation()
		if ai.run == nil {
			state.aiPanelReset()
		}
	case id == hitAIHistory:
		panel.history = !panel.history
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
	case id == hitAIModelChip:
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
func (state *State) aiTrackPointer(event ui.PointerEvent) (repaint bool) {
	panel := &state.ai.panel
	switch event.Kind {
	case ui.PointerDown, ui.PointerMove:
		panel.pointer = ui.Point{X: event.X, Y: event.Y}
	case ui.PointerExit:
		panel.pointer = ui.Point{}
	default:
		return false
	}
	over := pointInRect(panel.pointer.X, panel.pointer.Y, panel.answer)
	changed := over != panel.answerOver
	panel.answerOver = over
	return changed
}

// aiPanelPointer moves the focus to the message box when an example is released,
// and follows a drag of the panel's or the box's edge.
func (state *State) aiPanelPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	panel := &state.ai.panel
	if event.Kind == ui.PointerUp && event.Target.Scope() == hitAIExamples && event.Button == ui.PointerButtonPrimary {
		// The click that follows fills the prompt in; the box takes the focus so the
		// user can edit it or press Enter.
		return ui.EventResult{Focus: hitAIInput, Repaint: true}, true
	}
	if event.Kind == ui.PointerDown && (event.Target == hitAIDivider || event.Target == hitAIComposerGrip) {
		if event.Button != ui.PointerButtonPrimary && event.Button != ui.PointerButtonNone {
			return ui.EventResult{}, true
		}
		panel.drag = aiDragWidth
		if event.Target == hitAIComposerGrip {
			panel.drag, panel.dragFrom, panel.dragStart = aiDragHeight, event.Y, panel.boxHeight
		}
		return state.aiDrag(event), true
	}
	if panel.drag == aiDragNone {
		return ui.EventResult{}, false
	}
	switch event.Kind {
	case ui.PointerMove:
		if event.Captured {
			return state.aiDrag(event), true
		}
	case ui.PointerUp:
		result := state.aiDrag(event)
		panel.drag = aiDragNone
		state.save()
		result.Handled = true
		return result, true
	case ui.PointerExit:
		panel.drag = aiDragNone
		return ui.EventResult{Handled: true}, true
	}
	return ui.EventResult{Handled: true}, true
}

// aiDrag follows the pointer while the panel's left edge or the message box's top
// edge is being dragged.
func (state *State) aiDrag(event ui.PointerEvent) ui.EventResult {
	ai, panel := &state.ai, &state.ai.panel
	switch panel.drag {
	case aiDragWidth:
		least, most := state.aiPanelLimits(event.Viewport)
		ai.panelWidth = min(max(event.Viewport.Width-event.X-1, least), most)
		state.aiPanelSync()
	case aiDragHeight:
		ai.composerHeight = max(panel.dragStart+panel.dragFrom-event.Y, 0)
	}
	return ui.EventResult{Handled: true, Repaint: true}
}

// aiPanelScroll scrolls the message list, or the history list. A wheel over the
// rest of the panel is swallowed: it must not scroll the terminal behind it.
func (state *State) aiPanelScroll(event ui.ScrollEvent) (ui.EventResult, bool) {
	panel := &state.ai.panel
	if panel.modelMenu && pointInRect(event.X, event.Y, panel.modelsRect) {
		return ui.EventResult{Handled: true, Repaint: panel.modelList.Wheel(event)}, true
	}
	if panel.rect.Width <= 0 || !pointInRect(event.X, event.Y, panel.rect) {
		return ui.EventResult{}, false
	}
	if pointInRect(event.X, event.Y, panel.composer) {
		panel.inputLayout.ScrollBy(event.DeltaY)
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if !pointInRect(event.X, event.Y, panel.list) {
		return ui.EventResult{Handled: true}, true
	}
	step := event.DeltaY
	if !event.Precise && step > 0 && step < 24 {
		step = 24
	} else if !event.Precise && step < 0 && step > -24 {
		step = -24
	}
	return state.aiPanelScrollBy(step), true
}

// aiPanelScrollBy moves the list by step pixels. Reaching the end of the
// conversation makes it follow new messages again; leaving it stops that.
func (state *State) aiPanelScrollBy(step float32) ui.EventResult {
	ai := &state.ai
	panel := &ai.panel
	if panel.listing {
		before := panel.historyTop
		panel.historyTop = min(max(before+step, 0), panel.maxTop)
		return ui.EventResult{Handled: true, Repaint: panel.historyTop != before}
	}
	before := ai.scroll.Offset()
	ai.scroll.ScrollBy(step)
	return ui.EventResult{Handled: true, Repaint: ai.scroll.Offset() != before}
}

func (state *State) aiPanelKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if !inAIPanel(event.Target) || event.Target == hitAIToggle {
		return ui.EventResult{}, false
	}
	ai := &state.ai
	panel := &ai.panel
	if event.Target.Scope() == hitAIExamples && (event.Key == ui.KeyEnter || event.Key == ui.KeySpace) && !event.Repeat {
		return ui.EventResult{Focus: hitAIInput, Repaint: true}, true
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
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
					return ui.EventResult{Handled: true, Repaint: true, Focus: hitAIModels.At(0), FocusVisible: true}, true
				}
				return handled, true
			}
		}
		if command == components.SubmenuCommandClose && panel.modelSubmenu.IsAnyOpen() {
			trigger := panel.modelSubmenu.Trigger()
			panel.modelSubmenu = panel.modelSubmenu.Close()
			return ui.EventResult{Handled: true, Repaint: true, Focus: trigger, FocusVisible: true}, true
		}
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
	case ui.KeyEscape:
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
		if _, composing := ai.input.Composition(); composing && event.Target == hitAIInput {
			return ui.EventResult{}, false // the input method owns Escape
		}
		if event.Target == hitAIInput {
			// Back to the terminal the panel is about.
			return ui.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
		}
	case ui.KeyTab:
		// Tab takes the first session the @ before the caret could name.
		if event.Target == hitAIInput && !event.Repeat && event.Modifiers == 0 && len(panel.mentions) > 0 {
			state.acceptMention(panel.mentions[0].name)
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	case ui.KeyEnter:
		switch event.Target {
		case hitAIInput:
			if event.Repeat {
				return handled, true
			}
			if event.Modifiers&ui.ModifierShift != 0 {
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
	case ui.KeyPageUp, ui.KeyPageDown:
		if event.Target == hitAIInput || panel.list.Height <= 0 {
			return ui.EventResult{}, false
		}
		step := panel.list.Height * 0.9
		if event.Key == ui.KeyPageUp {
			step = -step
		}
		return state.aiPanelScrollBy(step), true
	}
	return ui.EventResult{}, false
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
// reaches the native editor as app.TextInsertLineBreak (see aiPanelText).
func (state *State) aiPanelShortcut(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Target != hitAIInput || event.Text != "j" || event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) == 0 ||
		event.Modifiers&^(ui.ModifierControl|ui.ModifierMeta) != 0 {
		return ui.EventResult{}, false
	}
	if !event.Repeat {
		state.ai.input.Insert("\n")
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

// aiPanelEditor serves the single-line fields of the panel: the word that a
// critical action asks to be typed. The message box is a multi-line editor and
// is served by AIPanelInput.
func (state *State) aiPanelEditor(hit ui.ID) *text.Editor {
	if hit == hitAIConfirm && len(state.ai.approvals) > 0 && state.ai.approvals[0].request.NeedsText {
		return &state.ai.confirm
	}
	return nil
}

// AIPanelInput resolves the native editor of the assistant's message box. It
// edits the same text.Editor the panel paints, but takes Enter itself: Enter
// (app.TextInsertNewline) sends the message instead of putting a line break in
// it, while Shift+Enter (app.TextInsertLineBreak) still inserts one.
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
	_, composing := ai.input.Composition()
	if event.Kind == app.TextCommandEvent && event.Command == app.TextInsertNewline && !composing {
		state.aiPanelEnter()
		result := app.TextStateOf(&ai.input)
		result.Redraw = true // the conversation changed, not only the editor
		return result
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
	quoted := strings.TrimSpace(session.term.screen.SelectedText(selection.anchor, selection.focus))
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
	state.openAIPanelWith(state.messages.AIPanel.PromptSelectionFn + "\n```\n" + quoted + "\n```\n")
}

// aiExplainOutput puts the question about the terminal's recent output in the
// message box; Ask mode sends the screen's text along with it.
func (state *State) aiExplainOutput() {
	state.setAIMode(agent.ModeAsk)
	state.openAIPanelWith(state.messages.AIPanel.PromptExplain)
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
