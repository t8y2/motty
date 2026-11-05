package workbench

import (
	"context"
	"time"

	"motty/internal/cmdhistory"
	"motty/internal/completion"
	"motty/internal/localshell"
	"motty/internal/shellkind"
)

const completionPathCache = 5 * time.Second

type completionPath struct {
	ready      chan completionPathResult
	cancel     context.CancelFunc
	generation uint64
	sessionID  uint32
	owner      *terminal
	conn       Connection
	query      completion.PathQuery
	pending    bool
	candidates []completion.Candidate
	retryAt    time.Duration
	expiresAt  time.Duration
}

type completionPathResult struct {
	generation uint64
	sessionID  uint32
	owner      *terminal
	conn       Connection
	query      completion.PathQuery
	candidates []completion.Candidate
	err        error
}

func (state *State) cancelCompletionPath() {
	current := &state.completion.path
	if current.cancel != nil {
		current.cancel()
	}
	*current = completionPath{generation: current.generation + 1}
}

func (state *State) stopCompletionPath() {
	current := &state.completion.path
	if current.pending {
		if current.cancel != nil {
			current.cancel()
		}
		current.cancel, current.ready, current.pending = nil, nil, false
		current.generation++
	}
}

func (state *State) completionPathCWD(session *Session) string {
	if session == nil || session.term == nil || !session.term.folderIntegrationSeen {
		return ""
	}
	cwd := session.term.reportedDirectory
	if !validSyncDirectory(cwd.Path) || session.isLocal() && !localDirectoryHost(cwd.Host) {
		return ""
	}
	return cwd.Path
}

func (state *State) completionCanPath(session *Session, line string) (completion.PathQuery, bool) {
	if !state.completion.probeEnabled || state.history.suggestOff || !state.completionEligible(session) ||
		!state.historyAvailable(session) || session.conn == nil || session.term.shellKind() != shellkind.POSIX ||
		cmdhistory.Secret(line) {
		return completion.PathQuery{}, false
	}
	if local, ok := session.conn.(*localConn); ok {
		if _, wsl := localshell.WSLDistro(local.shell); wsl {
			return completion.PathQuery{}, false
		}
	}
	return completion.ParsePathQuery(line, state.completionPathCWD(session))
}

func (state *State) startCompletionPath(session *Session, line string) {
	query, ok := state.completionCanPath(session, line)
	if !ok {
		return
	}
	current := &state.completion.path
	if current.sessionID != session.ID || current.owner != session.term || current.conn != session.conn || current.query != query {
		state.cancelCompletionPath()
		current.sessionID, current.owner, current.conn, current.query = session.ID, session.term, session.conn, query
	}
	now := state.options.Now()
	if current.expiresAt > now || current.pending || now < current.retryAt {
		return
	}
	var runner completion.CommandRunner = session.conn
	if local, ok := session.conn.(*localConn); ok {
		if _, wsl := localshell.WSLDistro(local.shell); wsl {
			return
		}
		runner = localshell.AvailabilityRunner{Program: local.shell, Dir: local.dir}
	}
	ctx, cancel := context.WithCancel(state.completionContext())
	current.cancel, current.pending = cancel, true
	current.generation++
	ready := make(chan completionPathResult, 1)
	current.ready = ready
	result := completionPathResult{generation: current.generation, sessionID: session.ID, owner: session.term, conn: session.conn, query: query}
	invalidate := state.options.Invalidate
	state.completion.workers.Add(1)
	go func() {
		defer state.completion.workers.Done()
		result.candidates, result.err = completion.ListPathCandidates(ctx, runner, query)
		ready <- result
		if invalidate != nil {
			invalidate()
		}
	}()
}

func (state *State) adoptCompletionPath() {
	current := &state.completion.path
	if !state.completion.probeEnabled || state.history.suggestOff {
		if current.conn != nil || current.pending {
			state.cancelCompletionPath()
		}
		return
	}
	if !current.pending || current.ready == nil {
		return
	}
	session, ok := state.sessionByIDPointer(current.sessionID)
	if !ok || session.term != current.owner || session.conn != current.conn || session.host.NoHistory ||
		state.completionPathCWD(session) != current.query.CWD {
		state.cancelCompletionPath()
		return
	}
	select {
	case result := <-current.ready:
		current.pending, current.ready = false, nil
		if current.cancel != nil {
			current.cancel()
			current.cancel = nil
		}
		list := &state.completion.list
		query, valid := state.completionCanPath(session, result.query.Line)
		if result.generation != current.generation || result.conn != session.conn || result.err != nil || !valid || query != result.query ||
			!list.active || list.sessionID != session.ID || list.owner != session.term || list.line != result.query.Line ||
			state.suggestionOwner(list.suggestionSnapshot) != session {
			current.retryAt = state.options.Now() + completionPathCache
			if list.active && list.sessionID == session.ID && len(list.candidates) == 0 {
				state.closeCompletionList()
			}
			return
		}
		current.candidates = result.candidates
		current.retryAt = 0
		current.expiresAt = state.options.Now() + completionPathCache
		if state.completion.preview.phase == previewIdle {
			state.completionRebuild(session)
		}
	default:
	}
}

func (state *State) cachedCompletionPath(session *Session, line string) []completion.Candidate {
	current := &state.completion.path
	if !state.completion.probeEnabled || session == nil || current.sessionID != session.ID ||
		current.owner != session.term || current.conn != session.conn || current.query.Line != line ||
		current.query.CWD != state.completionPathCWD(session) || current.expiresAt <= state.options.Now() {
		return nil
	}
	return current.candidates
}
