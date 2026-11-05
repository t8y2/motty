package workbench

import (
	"strings"
	"time"

	"motty/internal/cmdhistory"
	"motty/internal/completion"
	"motty/internal/shellkind"
)

const completionPreviewWait = 750 * time.Millisecond

type completionPreviewPhase uint8

const (
	previewIdle completionPreviewPhase = iota
	previewAwaitingEcho
	previewed
	previewRestoring
)

type completionPreview struct {
	phase             completionPreviewPhase
	sessionID         uint32
	owner             *terminal
	conn              Connection
	token             string
	epoch             uint64
	version           uint64
	historyGeneration uint64
	catalogGeneration uint64
	baseline          string
	shown             string
	desired           string
	restoreRequested  bool
	confirmRequested  bool
	startedAt         time.Duration
	timer             *time.Timer
}

func (state *State) cancelCompletionPreview() {
	if timer := state.completion.preview.timer; timer != nil {
		timer.Stop()
	}
	state.completion.preview = completionPreview{}
}

func (state *State) failCompletionPreview() {
	state.cancelCompletionPreview()
	state.closeCompletionList()
	state.status = state.messages.Completion.PreviewUncertain
}

func previewSimpleLine(line string) bool {
	if len(line) == 0 || len(line) > 256 {
		return false
	}
	for _, value := range line {
		if value < ' ' || value > '~' {
			return false
		}
	}
	return true
}

func previewFitsLine(promptCells int, line string, cols int) bool {
	return promptCells >= 0 && promptCells+len(line) < cols
}

func (state *State) previewCompletion(session *Session, index int) {
	list := &state.completion.list
	if !state.completion.previewEnabled || !list.active || index < 0 || index >= len(list.candidates) {
		return
	}
	candidate := list.candidates[index]
	if candidate.FromCatalog && !completion.SafeInsert(candidate.Insert) {
		if state.completion.preview.phase != previewIdle {
			state.restoreCompletionPreview(session)
		}
		return
	}
	preview := &state.completion.preview
	if preview.phase == previewIdle {
		if !state.completionEligible(session) || !session.term.integrated || !session.term.folderIntegrationSeen ||
			!session.term.inputGuard.known || !session.term.previewClearBound || session.term.shellKind() != shellkind.POSIX || session.term.prediction.visible() {
			return
		}
		current := state.suggestionOwner(list.suggestionSnapshot)
		if current == nil || current != session || !previewSimpleLine(list.line) {
			return
		}
		preview.sessionID, preview.owner, preview.conn = session.ID, session.term, session.conn
		preview.token, preview.epoch = session.term.folderToken, session.term.inputGuard.epoch
		preview.historyGeneration, preview.catalogGeneration = state.completion.generation, state.completion.catalogGeneration
		preview.baseline, preview.shown = list.line, list.line
	}
	target := preview.baseline + candidate.Insert
	if !previewSimpleLine(target) {
		state.restoreCompletionPreview(session)
		return
	}
	if preview.restoreRequested || preview.phase == previewRestoring {
		return
	}
	if preview.phase == previewAwaitingEcho {
		preview.desired = target
		return
	}
	if preview.phase == previewed && !state.confirmedPreviewLine(session) {
		state.failCompletionPreview()
		return
	}
	if target != preview.shown && !state.sendPreviewLine(session, target, previewAwaitingEcho) {
		state.failCompletionPreview()
	}
}

func (state *State) confirmedPreviewLine(session *Session) bool {
	preview := &state.completion.preview
	if session == nil || state.inputOwnerAfterDrain(session.ID, session.term) != session {
		return false
	}
	if preview.phase == previewIdle || session == nil || session.ID != preview.sessionID || session.term != preview.owner ||
		session.conn != preview.conn || !state.completion.previewEnabled || state.history.suggestOff || !state.completionEligible(session) ||
		!session.term.previewClearBound ||
		session.term.folderToken != preview.token || session.term.inputGuard.epoch != preview.epoch ||
		session.term.promptVersion != preview.version || session.term.preedit.Composing ||
		session.term.screen.Modes().AltScreen || state.broadcast != 0 || state.modal != modalNone ||
		state.completion.generation != preview.historyGeneration || state.completion.catalogGeneration != preview.catalogGeneration ||
		len(session.term.rx) > 0 {
		return false
	}
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	return ok && line == preview.shown
}

func (state *State) sendPreviewLine(session *Session, line string, phase completionPreviewPhase) bool {
	preview := &state.completion.preview
	if session == nil || state.inputOwnerAfterDrain(session.ID, session.term) != session {
		return false
	}
	if preview.phase == previewIdle {
		if state.suggestionOwner(state.completion.list.suggestionSnapshot) != session || !session.term.previewClearBound {
			return false
		}
	} else if !state.confirmedPreviewLine(session) {
		return false
	}
	terminal := session.term
	cols, _ := terminal.screen.Size()
	promptCells := terminal.screen.Cursor().X - len(preview.shown)
	if !previewSimpleLine(line) || !previewFitsLine(promptCells, line, cols) {
		return false
	}
	state.completion.previewWriting = true
	accepted := terminal.sendInput([]byte("\x15"+line), false)
	state.completion.previewWriting = false
	if !accepted {
		return false
	}
	if preview.timer != nil {
		preview.timer.Stop()
	}
	preview.phase, preview.shown, preview.version = phase, line, terminal.promptVersion
	preview.startedAt = state.options.Now()
	if invalidate := state.options.Invalidate; invalidate != nil {
		preview.timer = time.AfterFunc(completionPreviewWait, invalidate)
	}
	return true
}

func (state *State) restoreCompletionPreview(session *Session) bool {
	preview := &state.completion.preview
	if preview.phase == previewIdle || preview.sessionID != session.ID || preview.owner != session.term {
		return false
	}
	if state.inputOwnerAfterDrain(session.ID, session.term) != session || !state.completionEligible(session) || !session.term.previewClearBound ||
		session.conn != preview.conn || session.term.inputGuard.epoch != preview.epoch || len(session.term.rx) > 0 {
		state.failCompletionPreview()
		return true
	}
	if preview.phase == previewAwaitingEcho {
		preview.restoreRequested = true
		preview.desired = ""
		return true
	}
	if preview.phase == previewRestoring {
		return true
	}
	if !state.confirmedPreviewLine(session) || !state.sendPreviewLine(session, preview.baseline, previewRestoring) {
		state.failCompletionPreview()
	}
	return true
}

func (state *State) confirmCompletionPreview(session *Session) bool {
	preview := &state.completion.preview
	if preview.phase == previewIdle || preview.sessionID != session.ID || preview.owner != session.term {
		return false
	}
	if state.inputOwnerAfterDrain(session.ID, session.term) != session || !state.completionEligible(session) || !session.term.previewClearBound ||
		session.conn != preview.conn || session.term.inputGuard.epoch != preview.epoch || len(session.term.rx) > 0 {
		state.failCompletionPreview()
		return true
	}
	if preview.phase == previewAwaitingEcho {
		preview.confirmRequested = true
		return true
	}
	if preview.phase == previewRestoring {
		return true
	}
	if !state.confirmedPreviewLine(session) {
		state.failCompletionPreview()
		return true
	}
	state.cancelCompletionPreview()
	state.closeCompletionList()
	return true
}

func (state *State) reconcileCompletionPreview(session *Session) {
	preview := &state.completion.preview
	if preview.phase == previewIdle || preview.sessionID != session.ID {
		return
	}
	if session.term != preview.owner || session.conn != preview.conn || !state.completion.list.active ||
		!state.completion.previewEnabled || state.history.suggestOff || !state.completionEligible(session) || !session.term.previewClearBound ||
		session.term.folderToken != preview.token || session.term.inputGuard.epoch != preview.epoch ||
		session.term.promptVersion != preview.version || session.term.preedit.Composing ||
		session.term.screen.Modes().AltScreen || state.broadcast != 0 || state.modal != modalNone {
		state.failCompletionPreview()
		return
	}
	if preview.phase == previewed {
		return
	}
	if state.options.Now()-preview.startedAt > completionPreviewWait {
		state.failCompletionPreview()
		return
	}
	if len(session.term.rx) > 0 {
		return
	}
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	if !ok || line != preview.shown {
		if ok && !strings.HasPrefix(preview.shown, line) {
			state.failCompletionPreview()
		}
		return
	}
	if preview.timer != nil {
		preview.timer.Stop()
		preview.timer = nil
	}
	session.term.inputGuard.known = true
	session.term.inputGuard.prefix = line
	if preview.phase == previewRestoring {
		state.cancelCompletionPreview()
		state.closeCompletionList()
		return
	}
	preview.phase = previewed
	preview.version = session.term.promptVersion
	if preview.restoreRequested {
		if !state.sendPreviewLine(session, preview.baseline, previewRestoring) {
			state.failCompletionPreview()
		}
		return
	}
	if preview.desired != "" && preview.desired != line {
		selected := preview.desired
		preview.desired = ""
		if !state.sendPreviewLine(session, selected, previewAwaitingEcho) {
			state.failCompletionPreview()
		}
		return
	}
	if preview.confirmRequested {
		state.cancelCompletionPreview()
		state.closeCompletionList()
	}
}
