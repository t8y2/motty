package workbench

import (
	"context"
	"strings"
	"time"

	"motty/internal/completion"
	"motty/internal/localshell"
	"motty/internal/shellkind"
)

type completionDiscovery struct {
	ready      chan completionDiscoveryResult
	cancel     context.CancelFunc
	generation uint64
	sessionID  uint32
	owner      *terminal
	conn       Connection
	prefix     string
	pending    bool
	names      []string
	retryAt    time.Duration
	expiresAt  time.Duration
}

type completionDiscoveryResult struct {
	generation uint64
	sessionID  uint32
	owner      *terminal
	conn       Connection
	prefix     string
	names      []string
	err        error
}

func (state *State) cancelCompletionDiscovery() {
	discovery := &state.completion.discovery
	if discovery.cancel != nil {
		discovery.cancel()
	}
	*discovery = completionDiscovery{generation: discovery.generation + 1}
}

func (state *State) stopCompletionDiscovery() {
	discovery := &state.completion.discovery
	if discovery.pending {
		if discovery.cancel != nil {
			discovery.cancel()
		}
		discovery.cancel, discovery.ready, discovery.pending = nil, nil, false
		discovery.generation++
	}
}

func (state *State) completionCanDiscover(session *Session, line string) bool {
	if !state.completion.probeEnabled || state.history.suggestOff || !state.completionEligible(session) || !state.historyAvailable(session) ||
		session.conn == nil || !completion.ValidDiscoveryPrefix(line) || strings.ContainsAny(line, " \t") {
		return false
	}
	if local, ok := session.conn.(*localConn); ok {
		if _, wsl := localshell.WSLDistro(local.shell); wsl {
			return false
		}
	}
	kind := session.term.shellKind()
	return kind == shellkind.POSIX || kind == shellkind.PowerShell
}

func (state *State) startCompletionDiscovery(session *Session, line string) {
	if !state.completionCanDiscover(session, line) {
		return
	}
	discovery := &state.completion.discovery
	if discovery.sessionID != session.ID || discovery.owner != session.term || discovery.conn != session.conn || discovery.prefix != line {
		state.cancelCompletionDiscovery()
		discovery.sessionID, discovery.owner, discovery.conn, discovery.prefix = session.ID, session.term, session.conn, line
	}
	now := state.options.Now()
	if discovery.expiresAt > now || discovery.pending || now < discovery.retryAt {
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
	discovery.cancel, discovery.pending = cancel, true
	discovery.generation++
	ready := make(chan completionDiscoveryResult, 1)
	discovery.ready = ready
	result := completionDiscoveryResult{generation: discovery.generation, sessionID: session.ID, owner: session.term, conn: session.conn, prefix: line}
	kind, invalidate := session.term.shellKind(), state.options.Invalidate
	state.completion.workers.Add(1)
	go func() {
		defer state.completion.workers.Done()
		result.names, result.err = completion.DiscoverCommands(ctx, runner, kind, line)
		ready <- result
		if invalidate != nil {
			invalidate()
		}
	}()
}

func (state *State) adoptCompletionDiscovery() {
	discovery := &state.completion.discovery
	if !state.completion.probeEnabled || state.history.suggestOff {
		if discovery.conn != nil || discovery.pending {
			state.cancelCompletionDiscovery()
		}
		return
	}
	if !discovery.pending || discovery.ready == nil {
		return
	}
	session, ok := state.sessionByIDPointer(discovery.sessionID)
	if !ok || session.term != discovery.owner || session.conn != discovery.conn || session.host.NoHistory {
		state.cancelCompletionDiscovery()
		return
	}
	select {
	case result := <-discovery.ready:
		discovery.pending, discovery.ready = false, nil
		if discovery.cancel != nil {
			discovery.cancel()
			discovery.cancel = nil
		}
		list := &state.completion.list
		if result.generation != discovery.generation || result.conn != session.conn || result.err != nil ||
			!list.active || list.sessionID != session.ID || list.owner != session.term || list.line != result.prefix ||
			state.suggestionOwner(list.suggestionSnapshot) != session {
			discovery.retryAt = state.options.Now() + completionProbeCache
			if list.active && list.sessionID == session.ID && len(list.candidates) == 0 {
				state.closeCompletionList()
			}
			return
		}
		discovery.names = result.names
		discovery.retryAt = 0
		discovery.expiresAt = state.options.Now() + completionProbeCache
		if state.completion.preview.phase == previewIdle {
			state.completionRebuild(session)
		}
	default:
	}
}

func (state *State) discoveredCompletionCandidates(session *Session, line string) []completion.Candidate {
	discovery := &state.completion.discovery
	if !state.completion.probeEnabled || session == nil || discovery.sessionID != session.ID ||
		discovery.owner != session.term || discovery.conn != session.conn || discovery.prefix != line ||
		discovery.expiresAt <= state.options.Now() {
		return nil
	}
	candidates := make([]completion.Candidate, 0, len(discovery.names))
	for _, name := range discovery.names {
		match := strings.HasPrefix(name, line)
		if session.term.shellKind() == shellkind.PowerShell {
			match = len(name) >= len(line) && strings.EqualFold(name[:min(len(name), len(line))], line)
		}
		if match && len(name) > len(line) {
			suffix := name[len(line):]
			candidates = append(candidates, completion.Candidate{
				Text: line + suffix, Insert: suffix, Kind: completion.KindCommand, FromCatalog: true, FromHost: true,
			})
		}
	}
	return candidates
}

func (state *State) completionDiscoveredOnLine(session *Session, line string, candidate completion.Candidate) bool {
	discovery := &state.completion.discovery
	return candidate.FromHost && state.completion.probeEnabled && session != nil &&
		discovery.sessionID == session.ID && discovery.owner == session.term && discovery.conn == session.conn &&
		discovery.prefix == line && discovery.expiresAt > state.options.Now()
}
