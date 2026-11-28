package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

// aiMenuHits is the identity scope for the assistant conversation context menu.
var aiMenuHits = tree.Feature("ai-menu")

var (
	hitAIMenuDismiss     = tree.ID(aiMenuHits, "ai-menu-dismiss")
	hitAIMenuCopy        = tree.ID(aiMenuHits, "ai-menu-copy")
	hitAIMenuCopyCode    = tree.ID(aiMenuHits, "ai-menu-copy-code")
	hitAIMenuCopyMessage = tree.ID(aiMenuHits, "ai-menu-copy-message")
	hitAIMenuFollowUp    = tree.ID(aiMenuHits, "ai-menu-follow-up")
	hitAIMenuSelectAll   = tree.ID(aiMenuHits, "ai-menu-select-all")
)

// aiContextMenu holds the state of the right-click context menu over the
// assistant conversation list.
type aiContextMenu struct {
	open        bool
	at          gfx.Point
	row         int
	hasCode     bool
	codeText    string
	messageText string
	selectable  bool
}

// aiPanelMenuPointer opens the conversation context menu on a secondary press
// inside the assistant message list without disturbing an existing selection.
func (state *State) aiPanelMenuPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary {
		return gfx.EventResult{}, false
	}
	panel := &state.ai.panel
	if !state.aiPanelVisible() || panel.history || state.modal != modalNone || panel.rect.Width <= 0 {
		return gfx.EventResult{}, false
	}
	at := gfx.Point{X: event.X, Y: event.Y}
	if !pointInRect(at.X, at.Y, panel.list) || event.Target == hitAIMessageBar || event.Target == hitAIToEnd {
		return gfx.EventResult{}, false
	}
	row := state.aiMenuRowAt(event.Target, at)
	var (
		hasCode     bool
		codeText    string
		messageText string
		selectable  bool
	)
	if row >= 0 && row < len(state.ai.items) {
		messageText = state.aiRowCopyText(row)
		selectable = state.aiRowSelectable(row)
		if row < len(panel.rows) && panel.rows[row].kind == aiAssistant {
			codeText, hasCode = aiAssistantCodeAt(&panel.rows[row], at)
		}
	}
	if !panel.hasAITextSelection() && messageText == "" && !selectable {
		panel.contextMenu.open = false
		return gfx.EventResult{Handled: true}, true
	}
	panel.targetOpen = false
	panel.modelMenu = false
	panel.autoMenu = false
	panel.reasoningMenu = false
	panel.contextMenu = aiContextMenu{
		open:        true,
		at:          at,
		row:         row,
		hasCode:     hasCode,
		codeText:    codeText,
		messageText: messageText,
		selectable:  selectable,
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

// aiMenuRowAt resolves the conversation row behind target or window point at,
// returning -1 when the pointer rests in empty list space.
func (state *State) aiMenuRowAt(target gfx.ID, at gfx.Point) int {
	panel := &state.ai.panel
	if row, _, ok := aiTextTarget(target); ok && row >= 0 && row < len(state.ai.items) {
		return row
	}
	if row, ok := target.In(hitAIRows); ok && row >= 0 && row < len(state.ai.items) {
		return row
	}
	for index := range panel.paintedRows {
		if index < len(panel.rowSlots) && pointInRect(at.X, at.Y, panel.rowSlots[index].Rect()) {
			return panel.paintedRows[index].index
		}
	}
	return -1
}

// aiAssistantCodeAt returns the fenced code block under window point at, if any.
func aiAssistantCodeAt(view *aiRowView, at gfx.Point) (string, bool) {
	if view.kind != aiAssistant || view.textRect.Width <= 0 || view.textRect.Height <= 0 {
		return "", false
	}
	for _, block := range view.md.Codes() {
		rect := gfx.Rect{
			X:      view.textRect.X + block.Bounds.X,
			Y:      view.textRect.Y + block.Bounds.Y,
			Width:  block.Bounds.Width,
			Height: block.Bounds.Height,
		}
		if pointInRect(at.X, at.Y, rect) {
			return block.Text, true
		}
	}
	return "", false
}

// aiRowSelectable reports whether row has at least one visible selectable text block.
func (state *State) aiRowSelectable(row int) bool {
	panel := &state.ai.panel
	if row < 0 || row >= len(panel.rows) {
		return false
	}
	for part := aiTextPart(0); part < aiTextPartCount; part++ {
		if layout := panel.rows[row].textLayout(part); layout != nil && layout.TextLen() > 0 {
			return true
		}
	}
	return false
}

// aiRowCopyText returns the full text to copy or quote for conversation row.
func (state *State) aiRowCopyText(row int) string {
	if row < 0 || row >= len(state.ai.items) {
		return ""
	}
	item := &state.ai.items[row]
	switch item.kind {
	case aiAssistant, aiUser, aiNotice, aiReasoning:
		return item.text
	case aiTool:
		detail := aiToolDetail(item)
		switch {
		case detail != "" && item.result != "":
			return detail + "\n\n" + item.result
		case detail != "":
			return detail
		default:
			return item.result
		}
	}
	return ""
}

// declareAIMenu declares the assistant conversation context menu while open.
func (state *State) declareAIMenu(c ui.C) {
	panel := &state.ai.panel
	if !panel.contextMenu.open {
		return
	}
	if !state.aiPanelVisible() || panel.history || state.modal != modalNone {
		panel.contextMenu.open = false
		return
	}
	menu := &panel.contextMenu
	hasSelection := panel.hasAITextSelection()
	messages := state.messages
	aiMessages := messages.AIPanel
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	rows.add(components.ContextMenuItem{
		ID: hitAIMenuCopy, Label: messages.MenuCopy, Icon: icon(iconCopy), Disabled: !hasSelection,
	})
	rows.chord(state.shortcuts.chords.Chord(shortcutCopy))
	if menu.hasCode && menu.codeText != "" {
		rows.add(components.ContextMenuItem{
			ID: hitAIMenuCopyCode, Label: aiMessages.CopyCode, Icon: icon(iconCopy),
		})
	}
	rows.add(components.ContextMenuItem{
		ID: hitAIMenuCopyMessage, Label: aiMessages.MenuCopyMessage, Icon: icon(iconCopy), Disabled: menu.messageText == "",
	})
	rows.separate()
	followUpLabel := aiMessages.MenuFollowUpMessage
	if hasSelection {
		followUpLabel = aiMessages.MenuFollowUpSelection
	}
	rows.add(components.ContextMenuItem{
		ID: hitAIMenuFollowUp, Label: followUpLabel, Icon: icon(iconBot), Disabled: !hasSelection && menu.messageText == "",
	})
	rows.separate()
	rows.add(components.ContextMenuItem{
		ID: hitAIMenuSelectAll, Label: messages.MenuSelectAll, Icon: icon(iconSelectAll), Disabled: !menu.selectable,
	})
	rows.chord(keymap.Chord{Modifiers: keymap.Primary, Char: 'a'})
	state.declareRowsMenu(c, &panel.contextMenu.open, menu.at, hitAIMenuDismiss, &rows, state.chooseAIMenu)
}

// chooseAIMenu runs the action selected from the assistant conversation menu.
func (state *State) chooseAIMenu(id gfx.ID) {
	panel := &state.ai.panel
	menu := panel.contextMenu
	switch id {
	case hitAIMenuCopy:
		state.aiCopyTextSelection()
	case hitAIMenuCopyCode:
		if menu.codeText != "" {
			if err := state.options.SetClipboardText(menu.codeText); err != nil {
				state.ai.notice = err.Error()
			} else {
				panel.copied, panel.copiedAt = menu.codeText, state.now()
			}
		}
	case hitAIMenuCopyMessage:
		if menu.messageText != "" {
			if err := state.options.SetClipboardText(menu.messageText); err != nil {
				state.ai.notice = err.Error()
			} else {
				panel.copied, panel.copiedAt = menu.messageText, state.now()
			}
		}
	case hitAIMenuFollowUp:
		text := menu.messageText
		if selected, ok := panel.aiTextSelectionText(); ok {
			text = selected
		}
		state.aiFollowUpWith(text)
	case hitAIMenuSelectAll:
		if target, ok := state.aiSelectRow(menu.row); ok && !target.IsZero() {
			panel.focusTarget = target
		}
	}
}

// aiFollowUpWith inserts text as a Markdown blockquote into the assistant
// composer without sending it, and moves keyboard focus to the composer.
func (state *State) aiFollowUpWith(text string) {
	quoted := aiQuoteMarkdown(text)
	if quoted == "" {
		return
	}
	state.openAIPanelWith(quoted)
	state.ai.panel.focusTarget = hitAIInput
}

// aiQuoteMarkdown formats text as a Markdown blockquote followed by a blank line
// so the user can type a follow-up question directly underneath.
func aiQuoteMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.Trim(value, "\n")
	if strings.TrimSpace(value) == "" {
		return ""
	}
	const limit = 4000
	if len(value) > limit {
		cut := limit
		for cut > 0 && value[cut]&0xc0 == 0x80 {
			cut--
		}
		value = value[:cut] + "\n…"
	}
	var out strings.Builder
	for index, line := range strings.Split(value, "\n") {
		if index > 0 {
			out.WriteByte('\n')
		}
		if line == "" {
			out.WriteByte('>')
		} else {
			out.WriteString("> ")
			out.WriteString(line)
		}
	}
	out.WriteString("\n\n")
	return out.String()
}
