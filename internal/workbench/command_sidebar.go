package workbench

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/risk"
)

var commandSidebarHits = tree.Feature("command-sidebar")

var (
	hitCommandSidebarToggle = tree.ID(commandSidebarHits, "toggle")
	hitCommandSidebarAdd    = tree.ID(commandSidebarHits, "add")
	hitCommandSidebarManage = tree.ID(commandSidebarHits, "manage")
	hitCommandSidebarSort   = tree.ID(commandSidebarHits, "sort")
	hitCommandComposerFold  = tree.ID(commandSidebarHits, "compose-fold")
	hitCommandPinsFold      = tree.ID(commandSidebarHits, "pins-fold")
	hitCommandComposer      = tree.ID(commandSidebarHits, "compose")
	hitCommandComposeSend   = tree.ID(commandSidebarHits, "send")
	hitCommandComposeClear  = tree.ID(commandSidebarHits, "clear-after-send")
	hitCommandComposeAllow  = tree.ID(commandSidebarHits, "confirm-send")
	hitCommandSidebarRun    = tree.Sub(commandSidebarHits, tree.Name("run"))
	hitCommandSidebarPin    = tree.Sub(commandSidebarHits, tree.Name("pin"))
	hitCommandPinnedInsert  = tree.Sub(commandSidebarHits, tree.Name("insert"))
	hitCommandPinnedRun     = tree.Sub(commandSidebarHits, tree.Name("pinned-run"))
	hitCommandPinnedUnpin   = tree.Sub(commandSidebarHits, tree.Name("unpin"))
	hitSnippetPin           = tree.ID(snippetsHits, "pin")
)

// Only presentation and the draft are device-local. A pin belongs to the
// snippet record itself, so edits, renames, reordering and deletion cannot
// leave a second copy of a command behind.
type savedCommandSidebar struct {
	Collapsed         bool   `json:"collapsed,omitempty"`
	ComposerCollapsed bool   `json:"composer_collapsed,omitempty"`
	PinsCollapsed     bool   `json:"pins_collapsed,omitempty"`
	ClearAfterSend    bool   `json:"clear_after_send,omitempty"`
	Draft             string `json:"draft,omitempty"`
}

type commandSidebarUI struct {
	savedCommandSidebar
	editor       text.Editor
	layout       components.TextareaLayout
	input        app.TextInput
	focusDraft   bool
	order        [maxSnippets]int
	drawComposer func(*ui.DrawContext)
	// Approval is bound to both the exact text and terminal. Switching panes
	// or changing the draft requires a new review.
	approvedCommand string
	approvedSession uint32
	highlight       int // snippet index plus one
}

const (
	commandSidebarWidth = float32(288)
	commandSidebarRail  = float32(32)
)

func (s *State) commandSidebarExtent(available float32) float32 {
	if !s.commandsBarAvailable() {
		return 0
	}
	// Keep the same minimum terminal room as the assistant. At smaller
	// widths the rail returns automatically when other panels make space.
	if available < aiMinTerminal+commandSidebarRail {
		return 0
	}
	if s.commandSidebar.Collapsed || available < aiMinTerminal+commandSidebarWidth {
		return min(commandSidebarRail, available)
	}
	return min(commandSidebarWidth, max(commandSidebarRail, available*.4))
}

func (s *State) commandSidebarVisible() bool {
	if !s.commandsBarAvailable() || s.commandSidebar.Collapsed {
		return false
	}
	if s.viewport.Width > 0 {
		frame := s.workspacePixelBoundsWithoutCommands(s.deck.ActiveWorkspace(), s.viewport)
		return s.commandSidebarExtent(frame.Width) > commandSidebarRail
	}
	return true
}

func (s *State) restoreCommandSidebar(saved savedCommandSidebar) {
	// External config refreshes must not replace a draft being edited.
	if s.commandSidebar.editor.CommittedString() != saved.Draft {
		s.commandSidebar.editor = text.Editor{}
		s.commandSidebar.editor.Insert(cleanSnippetCommand(saved.Draft, 64*1024))
	}
	s.commandSidebar.savedCommandSidebar = saved
}

func (s *State) savedCommandSidebarState() savedCommandSidebar {
	saved := s.commandSidebar.savedCommandSidebar
	saved.Draft = s.commandSidebar.editor.CommittedString()
	return saved
}

func (s *State) clearComposeApproval() {
	s.commandSidebar.approvedCommand = ""
	s.commandSidebar.approvedSession = 0
}

func (s *State) insertCommandDraft(command string) {
	editor := &s.commandSidebar.editor
	editor.MoveTo(utf8.RuneCountInString(editor.String()), false)
	if editor.String() != "" && !strings.HasSuffix(editor.String(), "\n") {
		editor.Insert("\n")
	}
	editor.Insert(command)
	editor.BreakUndoGroup()
	s.commandSidebar.Collapsed = false
	s.commandSidebar.ComposerCollapsed = false
	s.commandSidebar.focusDraft = true
	s.clearComposeApproval()
	s.save()
}

func (s *State) insertSnippetDraft(index int) {
	if index < 0 || index >= len(s.snippets.list) {
		return
	}
	if snippetHasVariables(s.snippets.list[index].Command) {
		s.openSnippetVariablesDialog(index, s.activeSessionID, false)
		s.snippetPrompt.toComposer = true
		return
	}
	s.insertCommandDraft(s.snippets.list[index].Command)
}

func (s *State) sendCommandDraft(confirmed bool) {
	if !s.commandSidebarVisible() || s.modal != modalNone {
		return
	}
	v := &s.commandSidebar
	if marked, composing := v.editor.Composition(); composing && marked.Start != marked.End {
		return
	}
	command := cleanSnippetCommand(v.editor.String(), maxSnippetCommand)
	if len(v.editor.String()) > maxSnippetCommand {
		s.showActionWarning(s.messages.Tools.ComposeTitle, s.messages.Tools.ComposeTooLong)
		return
	}
	if command == "" {
		return
	}
	session, ok := s.sessionByIDPointer(s.activeSessionID)
	if !ok || session.term == nil || session.host.isDesktop() {
		s.showActionWarning(s.messages.Tools.ComposeTitle, s.messages.Tools.StatusSnippetNoTerminal)
		return
	}
	assessment := risk.Classify(command)
	if assessment.Level >= risk.High && (!confirmed || v.approvedCommand != command || v.approvedSession != session.ID) {
		v.approvedCommand, v.approvedSession = command, session.ID
		return
	}
	if s.sendResolvedSnippet(snippet{Name: s.messages.Tools.ComposeTitle, Run: true}, command, session) {
		if v.ClearAfterSend {
			v.editor.SetSelection(text.Range{Start: 0, End: utf8.RuneCountInString(v.editor.String())})
			v.editor.Insert("")
		}
		s.clearComposeApproval()
		s.save()
	}
}

// CommandComposerInput uses Viva's text editor for IME, selection, undo and
// multi-line input, and persists committed draft edits through Motty's store.
func (s *State) CommandComposerInput(context app.TextInputContext) *app.TextInput {
	if context.ID != hitCommandComposer || s.modal != modalNone || !s.commandSidebarVisible() || s.commandSidebar.ComposerCollapsed {
		return nil
	}
	v := &s.commandSidebar
	v.input = app.TextInput{State: app.TextStateOf(&v.editor), NavigationKeys: context.NavigationKeys}
	v.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		if s.closed || s.modal != modalNone || !s.commandSidebarVisible() || v.ComposerCollapsed {
			return app.TextState{}, nil
		}
		event.Apply(&v.editor)
		s.clearComposeApproval()
		if _, composing := v.editor.Composition(); !composing {
			s.save()
		}
		return app.TextStateOf(&v.editor), nil
	}
	return &v.input
}

func (s *State) commandComposerKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Target != hitCommandComposer || s.modal != modalNone || !s.commandSidebarVisible() {
		return gfx.EventResult{}, false
	}
	if event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEnter && event.Modifiers == gfx.ModifierControl {
		if !event.Repeat {
			s.sendCommandDraft(false)
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	v := &s.commandSidebar
	if _, composing := v.editor.Composition(); !composing {
		movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers)
		if movement != components.TextareaMoveNone {
			value := v.editor.String()
			to := v.layout.Move(value, aiByteOffset(value, v.editor.Caret()), movement)
			v.editor.MoveTo(utf8.RuneCountInString(value[:to]), extend)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}

func (s *State) clickCommandSidebar(id gfx.ID) bool {
	if !s.commandsBarAvailable() || s.modal != modalNone {
		return false
	}
	v := &s.commandSidebar
	switch {
	case id == hitCommandSidebarToggle:
		v.Collapsed = !v.Collapsed
	case id == hitCommandSidebarAdd:
		s.openSnippetDialog(-1)
	case id == hitCommandSidebarManage:
		s.activeSection = sectionSnippets
	case id == hitCommandSidebarSort:
		for index, order := range settingsCommandsSortChoices {
			if s.commands.sort == order {
				s.commands.sort = settingsCommandsSortChoices[(index+1)%len(settingsCommandsSortChoices)]
				break
			}
		}
	case id == hitCommandComposerFold:
		v.ComposerCollapsed = !v.ComposerCollapsed
	case id == hitCommandPinsFold:
		v.PinsCollapsed = !v.PinsCollapsed
	case id == hitCommandComposeClear:
		v.ClearAfterSend = !v.ClearAfterSend
	case id == hitCommandComposeSend:
		s.sendCommandDraft(false)
	case id == hitCommandComposeAllow:
		s.sendCommandDraft(true)
	case id.Scope() == hitCommandSidebarRun || id.Scope() == hitCommandPinnedRun:
		s.sendSnippet(item(id))
	case id.Scope() == hitCommandPinnedInsert:
		s.insertSnippetDraft(item(id))
	case id.Scope() == hitCommandSidebarPin || id.Scope() == hitCommandPinnedUnpin:
		index := item(id)
		if index >= 0 && index < len(s.snippets.list) {
			s.snippets.list[index].Pinned = !s.snippets.list[index].Pinned
		}
	default:
		return false
	}
	s.save()
	return true
}

func (s *State) openSnippetFromSelection(session *Session) {
	if session == nil || session.term == nil || !session.term.selection.selected {
		return
	}
	selected := session.term.selection.text(session.term.screen)
	if len(selected) > maxSnippetCommand {
		s.showActionWarning(s.messages.Tools.SnippetsTitle, s.messages.Tools.ComposeTooLong)
		return
	}
	command := cleanSnippetCommand(selected, maxSnippetCommand)
	if command == "" {
		return
	}
	s.openSnippetDialog(-1)
	form := &s.snippets
	form.command.Insert(command)
	first, _, _ := strings.Cut(strings.TrimSpace(command), "\n")
	form.name.Insert(cleanSnippetText(first, maxSnippetName))
	// A captured selection is reviewed before execution, just like a draft.
	form.run = false
	form.fromSelection = true
}

// The sidebar is a sibling of the panes, laid out with the application's
// existing components. Both lists scroll independently of the terminal.
func (s *State) declareCommandSidebar(c ui.C) {
	if !s.commandsBarAvailable() {
		return
	}
	frame := s.workspacePixelBoundsWithoutCommands(s.deck.ActiveWorkspace(), s.viewport)
	width := s.commandSidebarExtent(frame.Width)
	frame.Width = max(0, frame.Width-width)
	b := gfx.Rect{X: frame.X + frame.Width, Y: frame.Y, Width: width, Height: frame.Height}
	if b.Width <= 0 {
		return
	}
	v, m := &s.commandSidebar, s.messages.Tools
	c.Part("command-sidebar")
	ui.Column(c).Absolute().Left(b.X).Top(b.Y).Width(b.Width).Height(b.Height).
		Clip().Background(s.pal.workspace).Border(1, s.pal.line).Children(func() {
		if v.Collapsed || width <= commandSidebarRail {
			s.commandSidebarAction(c, hitCommandSidebarToggle, "‹", m.SidebarExpand, false)
			return
		}
		ui.Row(c).Height(36).FillWidth().Items(ui.Center).Children(func() {
			ui.Text(c, m.SnippetsTitle).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(s.pal.text).Ellipsis().Grow(1).MarginSides(0, 0, 0, 10)
			s.commandSidebarAction(c, hitCommandSidebarAdd, "+", m.SnippetAdd, false)
			s.commandSidebarAction(c, hitCommandSidebarManage, "…", m.SidebarManage, false)
			s.commandSidebarAction(c, hitCommandSidebarToggle, "›", m.SidebarCollapse, false)
		})
		s.commandSidebarAction(c, hitCommandSidebarSort, s.settingsCommandsSortLabel(s.commands.sort), m.CommandsSortRow, false)
		ui.Scroll(c).Grow(1).FillWidth().Scrollbars().Children(func() {
			for _, index := range s.sidebarCommandOrder() {
				s.declareCommandSidebarRow(c, index, false)
			}
			if len(s.snippets.list) == 0 {
				ui.Text(c, m.SnippetEmpty).MaxLines(0).FontSize(metrics.FontSizeS).Color(s.pal.muted).LineHeight(20).Margin(10)
			}
		})
		s.commandSidebarAction(c, hitCommandPinsFold, m.ComposePinned, m.ComposePinned, v.PinsCollapsed)
		if !v.PinsCollapsed {
			ui.Scroll(c).MaxHeight(100).FillWidth().Scrollbars().Children(func() {
				for index := range s.snippets.list {
					if s.snippets.list[index].Pinned {
						s.declareCommandSidebarRow(c, index, true)
					}
				}
			})
		}
		s.commandSidebarAction(c, hitCommandComposerFold, m.ComposeTitle, m.ComposePlaceholder, v.ComposerCollapsed)
		if !v.ComposerCollapsed {
			if v.drawComposer == nil {
				v.drawComposer = func(d *ui.DrawContext) { s.paintCommandComposer(d.Canvas, d.Rect) }
			}
			ui.Slot(c).Height(116).FillWidth().MarginSides(0, 8, 4, 8).Draw(v.drawComposer)
			ui.Row(c).Height(32).FillWidth().Items(ui.Center).Children(func() {
				s.commandSidebarAction(c, hitCommandComposeClear, m.ComposeClearAfter, m.ComposeClearAfter, v.ClearAfterSend)
				s.commandSidebarAction(c, hitCommandComposeSend, m.SendCommandButton, "Ctrl+Enter", false)
			})
			if v.approvedCommand != "" {
				ui.Text(c, m.ComposeRisk).MaxLines(0).FontSize(metrics.FontSizeS).Color(s.pal.warning).LineHeight(18).MarginSides(4, 8, 0, 8)
				if session, ok := s.sessionByIDPointer(s.activeSessionID); ok {
					ui.Text(c, session.Title).FontSize(metrics.FontSizeS).Color(s.pal.text).LineHeight(18).Ellipsis().MarginSides(0, 8, 0, 8)
				}
				s.commandSidebarAction(c, hitCommandComposeAllow, m.ComposeConfirm, m.ComposeConfirm, false)
			}
		}
	})
}

func (s *State) commandSidebarAction(c ui.C, id gfx.ID, label, hint string, selected bool) {
	fill := s.pal.workspace
	if selected || c.Canvas().Hovered() == id {
		fill = s.pal.surfaceHot
	}
	ui.Row(c).Height(30).MinWidth(28).PadX(6).Items(ui.Center).Background(fill).Radius(radius.control).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, SkipFocus: true}).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeS).Color(s.pal.text).Ellipsis()
	})
}

func (s *State) sidebarCommandOrder() []int {
	order := s.commandSidebar.order[:0]
	for index := range s.snippets.list {
		order = append(order, index)
	}
	if s.commands.sort != commandsSortCreated {
		slices.SortStableFunc(order, func(a, b int) int {
			left, right := &s.snippets.list[a], &s.snippets.list[b]
			if s.commands.sort == commandsSortGroup {
				if byGroup := cmp.Compare(left.sortGroup, right.sortGroup); byGroup != 0 {
					return byGroup
				}
			}
			return cmp.Compare(left.sortName, right.sortName)
		})
	}
	return order
}

func (s *State) declareCommandSidebarRow(c ui.C, index int, pinned bool) {
	entry, m := &s.snippets.list[index], s.messages.Tools
	action, pin := hitCommandSidebarRun.At(index), hitCommandSidebarPin.At(index)
	if pinned {
		action = hitCommandPinnedInsert.At(index)
	}
	ui.Row(c).Height(34).FillWidth().MarginSides(2, 6, 2, 6).Gap(3).Items(ui.Center).Children(func() {
		fill := s.pal.surfaceHot
		if s.commandSidebar.highlight == index+1 {
			fill = withAlpha(s.pal.accent, .16)
		}
		ui.Row(c).Grow(1).Height(30).PadX(8).Items(ui.Center).Background(fill).Radius(radius.control).
			HitWith(action, entry.Name, gfx.HitOptions{Role: gfx.RoleButton, Hint: entry.Command, SkipFocus: true}).Children(func() {
			ui.Text(c, entry.Name).FontSize(metrics.FontSizeS).Color(s.pal.text).Ellipsis().Grow(1)
		})
		if pinned {
			s.commandSidebarAction(c, hitCommandPinnedRun.At(index), "▶", m.SnippetSend, false)
			pin = hitCommandPinnedUnpin.At(index)
		}
		label, hint := "☆", m.ComposePin
		if entry.Pinned {
			label, hint = "★", m.ComposeUnpin
		}
		s.commandSidebarAction(c, pin, label, hint, entry.Pinned)
	})
}

func (s *State) paintCommandComposer(canvas *gfx.Canvas, bounds gfx.Rect) {
	v, m := &s.commandSidebar, s.messages.Tools
	placeholder := ""
	if canvas.Focused() == hitCommandComposer {
		placeholder = m.ComposePlaceholder
	}
	value, selection := v.editor.String(), v.editor.Selection()
	snapshot := components.TextareaSnapshot{
		ID: hitCommandComposer, Label: m.ComposeTitle, Value: value, Placeholder: placeholder,
		Focused: canvas.Focused() == hitCommandComposer, HideEditorText: true, ShowCaret: selection.Start == selection.End,
		Layout: &v.layout, SelectionStart: aiByteOffset(value, selection.Start), SelectionEnd: aiByteOffset(value, selection.End),
		Caret: aiByteOffset(value, v.editor.Caret()),
	}
	if marked, composing := v.editor.Composition(); composing {
		snapshot.CompositionStart, snapshot.CompositionEnd = aiByteOffset(value, marked.Start), aiByteOffset(value, marked.End)
	}
	components.PaintTextarea(canvas, s.Theme(), bounds, snapshot)
}
