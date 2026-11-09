package workbench

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/gate"
	layoutcore "motty/internal/workspace"
)

func (state *State) aiWatchTarget(ctx context.Context, run *aiRun, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		return
	case <-done:
		state.aiPost(func(s *State) {
			if s.ai.run == run {
				s.ai.notice = s.messages.AIPanel.TargetUnavailable
				run.cancel()
			}
		})
	}
}

var (
	hitAITarget        = tree.ID(aiPanelHits, "ai-target")
	hitAITargetDismiss = tree.ID(aiPanelHits, "ai-target-dismiss")
	hitAITargets       = tree.Sub(aiPanelHits, tree.Name("ai-targets"))
	// hitAITargetTabs rows select every terminal of one split tab.
	hitAITargetTabs = tree.Sub(aiPanelHits, tree.Name("ai-target-tabs"))
)

// maxAITargets is the connections one question can be about: the default plus
// as many more as a message may @mention.
const maxAITargets = 1 + maxMentions

func aiSessionInfo(session *Session) gate.SessionInfo {
	return gate.SessionInfo{ID: sessionHandle(session.ID), Title: session.Title, Address: session.Address,
		Ready: session.term != nil && session.term.shell != nil && session.term.live && !session.term.ended}
}

// Until the first send/explicit choice only the visible terminal is a default.
// A home/settings page never silently falls back to a background connection.
func (state *State) aiTarget() gate.SessionInfo {
	if state.ai.targetSet {
		if state.ai.target.ID != "" {
			if session, err := state.aiSession(state.ai.target.ID); err == nil {
				return aiSessionInfo(session)
			}
		}
		return state.ai.target
	}
	if state.activeSection == sectionTerminal {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.term != nil {
			return aiSessionInfo(session)
		}
	}
	return gate.SessionInfo{}
}

func (state *State) aiTargetSession() (*Session, error) {
	target := state.aiTarget()
	if target.ID == "" {
		return nil, errNoSession
	}
	return state.aiSession(target.ID)
}

func (state *State) aiTargetLabel(target gate.SessionInfo) string {
	if target.ID == "" {
		return state.messages.AIPanel.TargetNone
	}
	if target.Address == "" {
		return target.Title
	}
	return target.Title + " · " + target.Address
}

// aiConversationTargetLabel names the connections a question was about: the
// default first, then the others the user ticked.
func (state *State) aiConversationTargetLabel(target gate.SessionInfo, extras ...gate.SessionInfo) string {
	if target.ID == "" {
		return state.messages.AIPanel.TargetNone
	}
	label := target.Title
	for _, extra := range extras {
		label += ", " + extra.Title
	}
	return label
}

// aiExtraTargets are the connections ticked besides the default that are still
// open, with their current names. A closed one drops out instead of lingering as
// a stale entry that could never be reached.
func (state *State) aiExtraTargets() []gate.SessionInfo {
	if len(state.ai.extras) == 0 {
		return nil
	}
	primary := state.aiTarget().ID
	var live []gate.SessionInfo
	for _, extra := range state.ai.extras {
		if extra.ID == primary {
			continue
		}
		if session, err := state.aiSession(extra.ID); err == nil && session.term != nil {
			live = append(live, aiSessionInfo(session))
		}
	}
	return live
}

// aiExtraCount is len(aiExtraTargets()) without building the list, for painting.
func (state *State) aiExtraCount() int {
	count := 0
	primary := state.aiTarget().ID
	for _, extra := range state.ai.extras {
		if extra.ID == primary {
			continue
		}
		if session, err := state.aiSession(extra.ID); err == nil && session.term != nil {
			count++
		}
	}
	return count
}

// aiSelectedIDs is the ticked connections as session IDs, default first.
func (state *State) aiSelectedIDs() []uint32 {
	var ids []uint32
	add := func(info gate.SessionInfo) {
		if id, err := strconv.ParseUint(info.ID, 10, 32); err == nil {
			ids = append(ids, uint32(id))
		}
	}
	if target := state.aiTarget(); target.ID != "" {
		add(target)
	}
	for _, extra := range state.aiExtraTargets() {
		add(extra)
	}
	return ids
}

// aiSetSelected makes ids the ticked connections; the first becomes the default.
// Sessions that are gone are skipped.
func (state *State) aiSetSelected(ids []uint32) {
	state.dropEscalationApprovals()
	state.ai.retry, state.ai.answerOnly = false, false
	state.ai.target, state.ai.extras = gate.SessionInfo{}, nil
	for _, id := range ids {
		session, ok := state.sessionByIDPointer(id)
		if !ok || session.term == nil {
			continue
		}
		if state.ai.target.ID == "" {
			state.ai.target = aiSessionInfo(session)
		} else {
			state.ai.extras = append(state.ai.extras, aiSessionInfo(session))
		}
	}
	state.ai.targetSet = true
	state.ai.notice = ""
}

// A separator belongs to the first question sent to a target, not
// to every bubble or to temporary choices made before sending. User-message
// metadata reconstructs these boundaries when history is opened again.
func (state *State) aiAppendQuestion(question, key, label string) {
	changed := key != ""
	for index := len(state.ai.items) - 1; index >= 0; index-- {
		previous := &state.ai.items[index]
		if previous.kind != aiUser {
			continue
		}
		changed = previous.targetKey != key || previous.targetLabel != label
		break
	}
	if changed && label != "" {
		state.ai.items = append(state.ai.items, aiItem{kind: aiTargetChange, text: label})
	}
	state.ai.items = append(state.ai.items, aiItem{kind: aiUser, text: question, targetKey: key, targetLabel: label})
}

// aiRequestReferences are the connections besides the default that a question
// brings along: the ones ticked in the selector, then those it @mentions.
func (state *State) aiRequestReferences(question string) []gate.SessionInfo {
	var scratch [8]mentionName
	references := state.aiExtraTargets()
	for _, id := range findMentions(question, state.mentionNames(scratch[:0])) {
		session, ok := state.sessionByIDPointer(id)
		if !ok || session.term == nil {
			continue
		}
		info := aiSessionInfo(session)
		if !slices.ContainsFunc(references, func(have gate.SessionInfo) bool { return have.ID == info.ID }) {
			references = append(references, info)
		}
	}
	return references
}

func (state *State) aiTargetBackend(target gate.SessionInfo, references ...gate.SessionInfo) (*aiBackend, error) {
	backend := &aiBackend{state: state, scoped: true, defaultID: target.ID}
	bind := func(info gate.SessionInfo) error {
		session, err := state.aiSession(info.ID)
		if err != nil {
			return err
		}
		if !aiSessionInfo(session).Ready {
			return errNoShell
		}
		binding := aiSessionBinding{info: info, terminal: session.term, done: session.term.shell.Done()}
		select {
		case <-binding.done:
			return errNoShell
		default:
		}
		backend.bindings = append(backend.bindings, binding)
		if info.ID == target.ID {
			backend.defaultDone = binding.done
		}
		_, err = backend.session(info.ID)
		return err
	}
	var defaultErr error
	if target.ID != "" {
		defaultErr = bind(target)
	}
	for _, reference := range references {
		if reference.ID == "" || reference.ID == target.ID {
			continue
		}
		// A disconnected reference can still supply its text snapshot, but a
		// later reconnect must never add a fresh transport to this request.
		_ = bind(reference)
	}
	if defaultErr != nil && len(backend.bindings) == 0 {
		return nil, defaultErr
	}
	return backend, nil
}

// Runs on the window thread immediately before resolving a real operation.
func (backend *aiBackend) session(handle string) (*Session, error) {
	if !backend.scoped {
		return backend.state.aiSession(handle)
	}
	if handle == "" {
		handle = backend.defaultID
	}
	for _, binding := range backend.bindings {
		if binding.info.ID != handle {
			continue
		}
		session, err := backend.state.aiSession(handle)
		if err != nil {
			return nil, err
		}
		if session.term != binding.terminal || !aiSessionInfo(session).Ready || session.term.shell.Done() != binding.done {
			return nil, errors.New("target connection changed or disconnected; send a new request after reconnecting")
		}
		select {
		case <-binding.done:
			return nil, errors.New("target connection ended; send a new request after reconnecting")
		default:
			return session, nil
		}
	}
	return nil, errNoSession
}

// aiTabTerminals are the open terminals of a workspace tab.
func (state *State) aiTabTerminals(workspace layoutcore.WorkspaceID) []uint32 {
	var panes [8]layoutcore.Pane
	var ids []uint32
	for _, pane := range state.deck.Panes(workspace, panes[:0]) {
		if session, ok := state.sessionByIDPointer(uint32(pane.SessionID)); ok && session.term != nil && aiSessionInfo(session).Ready {
			ids = append(ids, session.ID)
		}
	}
	return ids
}

func (state *State) aiTargetOptions() []components.SelectItem {
	m := state.messages.AIPanel
	selected := state.aiSelectedIDs()
	options := []components.SelectItem{{ID: hitAITargets.At(0), Label: m.TargetNone, Selected: len(selected) == 0}}
	for _, tab := range state.workspaceTabs {
		ids := state.aiTabTerminals(tab.ID)
		if len(ids) < 2 {
			continue
		}
		all := true
		for _, id := range ids {
			all = all && slices.Contains(selected, id)
		}
		options = append(options, components.SelectItem{ID: hitAITargetTabs.At(int(tab.ID)),
			Label: fmt.Sprintf(m.TargetGroupFmt, state.tabLabel(tab), len(ids)), Selected: all,
			Icon: lucideIcons[iconTerminal]})
	}
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.term == nil {
			continue
		}
		info := aiSessionInfo(session)
		status := m.TargetConnected
		if !info.Ready {
			status = m.TargetDisconnected
		}
		options = append(options, components.SelectItem{ID: hitAITargets.At(int(session.ID)),
			Label: info.Title, Hint: state.aiTargetLabel(info) + " · " + status, Disabled: !info.Ready,
			Selected: slices.Contains(selected, session.ID),
			Icon:     state.hostIconArt(session.host.Icon, session.host.System, session.host.isDesktop())})
	}
	return options
}

// aiChooseTarget handles a click in the open target list: a connection or a
// whole tab is ticked or unticked and the list stays open for the next one. The
// first ticked connection is the default; unticking it promotes the next.
func (state *State) aiChooseTarget(id gfx.ID) bool {
	if state.ai.run != nil {
		return false
	}
	if _, ok := id.In(hitAITargets); ok {
		if index, _ := id.In(hitAITargets); index == 0 {
			state.aiSetSelected(nil)
			state.ai.panel.targetOpen = false
			return true
		}
	}
	ids, ok := state.aiTargetRowSessions(id)
	if !ok {
		return false
	}
	selected := state.aiSelectedIDs()
	all := true
	for _, id := range ids {
		all = all && slices.Contains(selected, id)
	}
	if all {
		selected = slices.DeleteFunc(selected, func(have uint32) bool { return slices.Contains(ids, have) })
		state.aiSetSelected(selected)
		return true
	}
	for _, id := range ids {
		if slices.Contains(selected, id) {
			continue
		}
		if len(selected) >= maxAITargets {
			state.aiSetSelected(selected)
			state.ai.notice = fmt.Sprintf(state.messages.AIPanel.TargetLimitFmt, maxAITargets)
			return true
		}
		selected = append(selected, id)
	}
	state.aiSetSelected(selected)
	return true
}

// aiChooseOnlyTarget is what typing a name on the closed selector does: that
// connection or tab becomes the whole selection.
func (state *State) aiChooseOnlyTarget(id gfx.ID) bool {
	if state.ai.run != nil {
		return false
	}
	if index, ok := id.In(hitAITargets); ok && index == 0 {
		return state.aiChooseTarget(id)
	}
	ids, ok := state.aiTargetRowSessions(id)
	if !ok {
		return false
	}
	state.aiSetSelected(ids[:min(len(ids), maxAITargets)])
	state.ai.panel.targetOpen = false
	return true
}

// aiTargetRowSessions are the ready sessions a list row stands for.
func (state *State) aiTargetRowSessions(id gfx.ID) ([]uint32, bool) {
	if index, ok := id.In(hitAITargets); ok {
		session, exists := state.sessionByIDPointer(uint32(index))
		if !exists || !aiSessionInfo(session).Ready {
			return nil, false
		}
		return []uint32{session.ID}, true
	}
	if index, ok := id.In(hitAITargetTabs); ok {
		ids := state.aiTabTerminals(layoutcore.WorkspaceID(index))
		return ids, len(ids) > 0
	}
	return nil, false
}

// Keep the numeric disambiguator visible even when a long tab title is elided.
func (panel *aiPanel) targetCaption(canvas *gfx.Canvas, title string, size, width float32) string {
	if panel.targetTitle == title && panel.targetWidth == width && panel.targetFontSize == size {
		return panel.targetValue
	}
	original, value := title, title
	if canvas.TextWidthWeight(title, size, gfx.WeightRegular) > width {
		suffix := ""
		if at := strings.LastIndexByte(title, '-'); at >= 0 {
			if number, err := strconv.Atoi(title[at+1:]); err == nil && number >= 2 {
				suffix, title = title[at:], title[:at]
			}
		}
		value = elide(canvas, title, size, gfx.WeightRegular, max(width-canvas.TextWidthWeight(suffix, size, gfx.WeightRegular), 0)) + suffix
	}
	panel.targetTitle, panel.targetWidth, panel.targetFontSize, panel.targetValue = original, width, size, value
	return value
}

// aiTargetListWidth is the open list's width: as wide as its longest row needs
// (item inset, icon, label and the tick), never beyond 320 or below the select's
// own minimum.
func aiTargetListWidth(canvas *gfx.Canvas, options []components.SelectItem, size float32) float32 {
	const chrome, minimum, maximum = 8 + 24 + 40 + 8, 176, 320 // inset, icon and gap, tick column, popup padding
	widest := float32(0)
	for _, option := range options {
		widest = max(widest, canvas.TextWidthWeight(option.Label, size, gfx.WeightRegular))
	}
	return min(max(widest+chrome, minimum), maximum)
}

func (state *State) paintAITargetSelect(canvas *gfx.Canvas) {
	panel, m := &state.ai.panel, state.messages.AIPanel
	bounds := panel.targetBounds
	if !state.aiPanelVisible() || bounds.Width <= 0 || state.modal != modalNone {
		panel.targetOpen = false
		return
	}
	if state.ai.run != nil {
		panel.targetOpen = false
	}
	target := state.aiTarget()
	value, hint := m.TargetChoose, m.TargetHint
	var suffix string // how many more connections are ticked, such as " +2"
	var moreWidth float32
	icon := lucideIcons[iconTerminal]
	if target.ID != "" {
		status := m.TargetConnected
		session, err := state.aiTargetSession()
		if err == nil {
			icon = state.hostIconArt(session.host.Icon, session.host.System, session.host.isDesktop())
		}
		if err != nil || !aiSessionInfo(session).Ready {
			status = m.TargetDisconnected
		} else if state.ai.run != nil {
			status = m.TargetLocked
		}
		value = target.Title
		hint = state.aiTargetLabel(target) + " · " + status
		if more := state.aiExtraCount(); more > 0 {
			more := " +" + strconv.Itoa(more)
			moreWidth = canvas.TextWidthWeight(more, metrics.FontSizeS, gfx.WeightRegular)
			suffix = more
			hint = state.aiConversationTargetLabel(target, state.aiExtraTargets()...) + " · " + status
		}
		hint += ". " + m.TargetHint
	}
	const targetChromeWidth = float32(66) // select text insets, leading icon and chevron
	// The selector sits in the composer among pills that are one type step below
	// the message text, so it is the select's chip step rather than a field.
	size := metrics.FontSizeS
	bounds.Width = min(bounds.Width, max(108, canvas.TextWidthWeight(value, size, gfx.WeightRegular)+moreWidth+targetChromeWidth))
	value = panel.targetCaption(canvas, value, size, bounds.Width-targetChromeWidth-moreWidth) + suffix
	panel.targetBounds = bounds
	var options []components.SelectItem
	if panel.targetOpen {
		options = state.aiTargetOptions()
	}
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(options)*32), max(above, below))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: min(aiTargetListWidth(canvas, options, state.Theme().Metrics.FontSizeM), state.viewport.Width-16), Height: height}
	content.X = min(max(content.X, 8), state.viewport.Width-8-content.Width)
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	// A narrow panel is a modal drawer. Its trigger must remain in that focus
	// scope even though the menu is painted outside the body's clip. The trigger
	// belongs to the drawer's layer, below dialogs and overlay menus, so an open
	// menu above it does not have the trigger's text show through.
	//
	// Its own list still has to cover the drawer, which paints before the modal
	// pass: the list stays on dialogLayer+1, between the drawer's text and a
	// dialog. Lowering it to drawerLayer to fix the bleed would move the problem
	// to the list instead of solving it.
	layer := popupLayer
	if state.aiReserve(state.deck.ActiveWorkspace(), state.viewport) == 0 {
		layer = dialogLayer + 1
		canvas.PushFocusScope(dialogScope)
		defer canvas.PopFocusScope()
		canvas.PushLayer(drawerLayer)
		defer canvas.PopLayer()
	}
	theme := state.Theme()
	theme.Input = withAlpha(state.pal.muted, 0.5)
	control := components.BeginSelect(canvas, theme, bounds, components.SelectOptions{
		TriggerID: hitAITarget, Label: m.Target, Value: value, Hint: hint, Open: panel.targetOpen,
		Size: components.SelectSizeXSmall, Icon: icon,
		Disabled: state.ai.run != nil, Content: content, Layer: layer,
		DismissID: hitAITargetDismiss, Scope: hitAITarget, Viewport: state.windowRect(), Scroll: state.selectScroll(panel.targetOpen, hitAITargetDismiss),
	})
	defer control.End()
	if panel.targetOpen {
		state.menuPlaced(control.ContentBounds())
	}
	for _, option := range options {
		control.AddItem(option)
	}
}
