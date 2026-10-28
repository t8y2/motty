package workbench

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/gate"
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
)

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

func (state *State) aiConversationTargetLabel(target gate.SessionInfo) string {
	if target.ID == "" {
		return state.messages.AIPanel.TargetNone
	}
	return target.Title
}

// A separator belongs to the first question sent to a different target, not
// to every bubble or to temporary choices made before sending. User-message
// metadata reconstructs these boundaries when history is opened again.
func (state *State) aiAppendQuestion(question, key, label string) {
	for index := len(state.ai.items) - 1; index >= 0; index-- {
		previous := &state.ai.items[index]
		if previous.kind != aiUser {
			continue
		}
		if previous.targetLabel != "" && label != "" && (previous.targetKey != key || previous.targetLabel != label) {
			state.ai.items = append(state.ai.items, aiItem{kind: aiTargetChange, text: label})
		}
		break
	}
	state.ai.items = append(state.ai.items, aiItem{kind: aiUser, text: question, targetKey: key, targetLabel: label})
}

func (state *State) aiTargetBackend(target gate.SessionInfo) (*aiBackend, error) {
	backend := &aiBackend{state: state}
	if target.ID == "" {
		return backend, nil
	}
	session, err := state.aiSession(target.ID)
	if err != nil {
		return nil, err
	}
	if !aiSessionInfo(session).Ready {
		return nil, errNoShell
	}
	backend.boundID, backend.boundTerminal, backend.boundDone = target.ID, session.term, session.term.shell.Done()
	if _, err := backend.session(target.ID); err != nil {
		return nil, err
	}
	return backend, nil
}

// Runs on the window thread immediately before resolving a real operation.
func (backend *aiBackend) session(handle string) (*Session, error) {
	if backend.boundID != "" && handle != backend.boundID {
		return nil, errNoSession
	}
	session, err := backend.state.aiSession(handle)
	if err != nil || backend.boundID == "" {
		return session, err
	}
	if session.term != backend.boundTerminal || !aiSessionInfo(session).Ready || session.term.shell.Done() != backend.boundDone {
		return nil, errors.New("target connection changed or disconnected; send a new request after reconnecting")
	}
	select {
	case <-backend.boundDone:
		return nil, errors.New("target connection ended; send a new request after reconnecting")
	default:
		return session, nil
	}
}

func (state *State) aiTargetOptions() []components.SelectItem {
	m := state.messages.AIPanel
	options := []components.SelectItem{{ID: hitAITargets.At(0), Label: m.TargetNone}}
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
			Icon: lucideIcons[iconTerminal]})
	}
	return options
}

func (state *State) aiChooseTarget(id ui.ID) bool {
	index, ok := id.In(hitAITargets)
	if !ok || state.ai.run != nil {
		return false
	}
	target := gate.SessionInfo{}
	if index != 0 {
		session, exists := state.sessionByIDPointer(uint32(index))
		if !exists || !aiSessionInfo(session).Ready {
			return false
		}
		target = aiSessionInfo(session)
	}
	state.dropEscalationApprovals()
	state.ai.retry, state.ai.answerOnly = false, false
	state.ai.target, state.ai.targetSet = target, true
	state.ai.notice, state.ai.panel.targetOpen = "", false
	return true
}

// Keep the numeric disambiguator visible even when a long tab title is elided.
func (panel *aiPanel) targetCaption(canvas *ui.Canvas, title string, size, width float32) string {
	if panel.targetTitle == title && panel.targetWidth == width && panel.targetFontSize == size {
		return panel.targetValue
	}
	original, value := title, title
	if canvas.TextWidthWeight(title, size, ui.WeightRegular) > width {
		suffix := ""
		if at := strings.LastIndexByte(title, '-'); at >= 0 {
			if number, err := strconv.Atoi(title[at+1:]); err == nil && number >= 2 {
				suffix, title = title[at:], title[:at]
			}
		}
		value = elide(canvas, title, size, ui.WeightRegular, max(width-canvas.TextWidthWeight(suffix, size, ui.WeightRegular), 0)) + suffix
	}
	panel.targetTitle, panel.targetWidth, panel.targetFontSize, panel.targetValue = original, width, size, value
	return value
}

func (state *State) paintAITargetSelect(canvas *ui.Canvas) {
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
	if target.ID != "" {
		status := m.TargetConnected
		session, err := state.aiTargetSession()
		if err != nil || !aiSessionInfo(session).Ready {
			status = m.TargetDisconnected
		} else if state.ai.run != nil {
			status = m.TargetLocked
		}
		value = target.Title
		hint = state.aiTargetLabel(target) + " · " + status + ". " + m.TargetHint
	}
	const targetChromeWidth = float32(66) // select text insets, leading icon and chevron
	size := state.Theme().Metrics.FontSizeM
	bounds.Width = min(bounds.Width, max(108, canvas.TextWidthWeight(value, size, ui.WeightRegular)+targetChromeWidth))
	value = panel.targetCaption(canvas, value, size, bounds.Width-targetChromeWidth)
	panel.targetBounds = bounds
	var options []components.SelectItem
	if panel.targetOpen {
		options = state.aiTargetOptions()
	}
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(options)*32), max(above, below))
	content := ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: min(320, state.viewport.Width-16), Height: height}
	content.X = min(max(content.X, 8), state.viewport.Width-8-content.Width)
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	// A narrow panel is a modal drawer. Its trigger must remain in that focus
	// scope even though the menu is painted outside the body's clip.
	layer := popupLayer
	if state.aiReserve(state.deck.ActiveWorkspace(), state.viewport) == 0 {
		layer = dialogLayer + 1
		canvas.PushFocusScope(dialogScope)
		defer canvas.PopFocusScope()
		canvas.PushLayer(dialogLayer)
		defer canvas.PopLayer()
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitAITarget, Label: m.Target, Value: value, Hint: hint, Open: panel.targetOpen,
		Size: components.SelectSizeSmall, Icon: lucideIcons[iconTerminal],
		Disabled: state.ai.run != nil, Content: content, Layer: layer,
		DismissID: hitAITargetDismiss, Scope: hitAITarget, Viewport: state.windowRect(), Scroll: state.selectScroll(panel.targetOpen, hitAITargetDismiss),
	})
	defer control.End()
	if panel.targetOpen {
		state.menuPlaced(control.ContentBounds())
	}
	selected, _ := strconv.Atoi(target.ID)
	for _, option := range options {
		option.Selected = option.ID == hitAITargets.At(selected)
		control.AddItem(option)
	}
}
