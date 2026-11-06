package workbench

import (
	"strings"
	"time"

	"motty/internal/cmdhistory"
)

const (
	autoCompletionQuiet   = 100 * time.Millisecond
	autoCompletionMaxWait = 3 * time.Second
)

type completionAuto struct {
	pending     bool
	ask         terminalInputConfirmation
	requestedAt time.Duration
	timer       *time.Timer
}

func (state *State) cancelAutoCompletion() {
	if timer := state.completion.auto.timer; timer != nil {
		timer.Stop()
	}
	state.completion.auto = completionAuto{}
}

func (state *State) scheduleAutoCompletion(session *Session) {
	state.cancelAutoCompletion()
	if !state.completion.autoPopup || state.history.suggestOff || !state.completionEligible(session) || !session.term.inputGuard.known {
		return
	}
	auto := &state.completion.auto
	auto.pending = true
	auto.ask = state.inputAsk(session)
	auto.requestedAt = state.options.Now()
	if invalidate := state.options.Invalidate; invalidate != nil {
		auto.timer = time.AfterFunc(autoCompletionQuiet, invalidate)
	}
}

func (state *State) adoptAutoCompletion() {
	auto := &state.completion.auto
	if !auto.pending {
		return
	}
	if !state.completion.autoPopup || state.history.suggestOff || state.options.Now()-auto.requestedAt > autoCompletionMaxWait {
		state.cancelAutoCompletion()
		return
	}
	ask := auto.ask
	session, ok := state.sessionByIDPointer(ask.sessionID)
	if !ok || session.term != ask.owner || !state.completionEligible(session) {
		state.cancelAutoCompletion()
		return
	}
	terminal := session.term
	if terminal.folderToken != ask.token || terminal.inputGuard.epoch != ask.epoch || terminal.promptVersion != ask.version ||
		!terminal.inputGuard.known || len(terminal.rx) > 0 {
		state.cancelAutoCompletion()
		return
	}
	if state.options.Now()-auto.requestedAt < autoCompletionQuiet {
		return
	}
	line, confirmed := terminal.recorder.Line(cmdhistory.TermScreen{Term: terminal.screen})
	if !confirmed || line != terminal.inputGuard.prefix || strings.TrimSpace(line) == "" || cmdhistory.Secret(line) {
		return
	}
	state.cancelAutoCompletion()
	state.openCompletionList(session)
}
