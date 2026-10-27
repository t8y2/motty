package workbench

import (
	"context"
	"errors"
	"strconv"

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
			Label: state.aiTargetLabel(info) + " · " + status, Disabled: !info.Ready})
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
	value := m.Target + ": " + state.aiTargetLabel(target)
	if target.ID != "" {
		status := m.TargetConnected
		session, err := state.aiTargetSession()
		if err != nil || !aiSessionInfo(session).Ready {
			status = m.TargetDisconnected
		} else if state.ai.run != nil {
			status = m.TargetLocked
		}
		value = m.Target + ": " + target.Title + " · " + status
	}
	var options []components.SelectItem
	if panel.targetOpen {
		options = state.aiTargetOptions()
	}
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(options)*32), max(above, below))
	content := ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
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
		TriggerID: hitAITarget, Label: m.Target, Value: value, Hint: m.TargetHint, Open: panel.targetOpen,
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
