package workbench

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

const (
	maxRemoteLogPaths    = 64
	remoteLogLineLimit   = 64 << 10
	remoteLogBufferSize  = 2 << 20
	remoteLogBufferLines = 5000
)

type remoteLogPath struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (p remoteLogPath) valid() bool {
	return p.Name != "" && len(p.Name) <= 256 && strings.HasPrefix(p.Path, "/") &&
		len(p.Path) <= 4096 && utf8.ValidString(p.Name) && utf8.ValidString(p.Path) &&
		!strings.ContainsFunc(p.Name, unicode.IsControl) && !strings.ContainsFunc(p.Path, unicode.IsControl)
}

func restoreRemoteLogPaths(paths []remoteLogPath) []remoteLogPath {
	var restored []remoteLogPath
	for _, path := range paths {
		if path.valid() && !slices.ContainsFunc(restored, func(other remoteLogPath) bool { return other.Path == path.Path }) {
			restored = append(restored, path)
			if len(restored) == maxRemoteLogPaths {
				break
			}
		}
	}
	return restored
}

func remoteLogCommand(path string) string {
	// A leading empty line acknowledges that the initial file is readable.
	// Keep tail as the session process so canceling can signal it directly.
	file := shellQuote(path)
	return "{ if [ ! -f " + file + " ]; then printf '%s\\n' 'Log path is missing or is not a regular file.'; exit 1; fi; " +
		"if [ ! -r " + file + " ]; then printf '%s\\n' 'Permission denied.'; exit 1; fi; " +
		"printf '\\n'; exec tail -n 200 -F -- " + file + "; } 2>&1"
}

// remoteLogBuffer is owned by one stream. The window sees immutable snapshots
// identified by the absolute sequence number of their first line.
type remoteLogBuffer struct {
	mu          sync.Mutex
	lines       [remoteLogBufferLines]string
	first, next int64
	bytes       int
	revision    uint64
	ready       bool
	done        bool
	err         error
}

func (b *remoteLogBuffer) append(raw []byte) bool {
	line := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	line = strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, line)
	if len(line) > remoteLogBufferSize {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for b.next-b.first == remoteLogBufferLines || b.bytes+len(line) > remoteLogBufferSize {
		index := b.first % remoteLogBufferLines
		b.bytes -= len(b.lines[index])
		b.lines[index] = ""
		b.first++
	}
	b.lines[b.next%remoteLogBufferLines] = line
	b.next++
	b.bytes += len(line)
	b.revision++
	return true
}

type remoteLogSnapshot struct {
	lines    []string
	first    int64
	revision uint64
	ready    bool
	done     bool
	err      error
}

func (b *remoteLogBuffer) snapshot(s *remoteLogSnapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.revision == b.revision {
		return
	}
	s.revision, s.first, s.ready, s.done, s.err = b.revision, b.first, b.ready, b.done, b.err
	clear(s.lines)
	s.lines = slices.Grow(s.lines[:0], int(b.next-b.first))
	for index := b.first; index < b.next; index++ {
		s.lines = append(s.lines, b.lines[index%remoteLogBufferLines])
	}
}

type remoteLogView struct {
	cancel             context.CancelFunc
	buffer             *remoteLogBuffer
	snapshot           remoteLogSnapshot
	selected           remoteLogPath
	paths              ui.ListState
	pathSplit          ui.SplitterState
	pathMenu           bool
	pathMenuFor        remoteLogPath
	query              ui.Field
	name, path         ui.Field
	formMode           remoteLogForm
	formOriginal       remoteLogPath
	formError          string
	follow             bool
	top                int64
	height             float32
	expanded, resizing bool
	pointerOffset      float32
	visibleRows        int
	matches            []int
	matchQuery         string
	matchRevision      uint64
	scrollRemainder    float32
	scroll             ui.Handlers
	scrollbar          components.ScrollAreaGeometry
	scrollGrab         float32
	scrollDragging     bool
}

type remoteLogForm uint8

const (
	remoteLogFormNone remoteLogForm = iota
	remoteLogFormAdd
	remoteLogFormEdit
	remoteLogFormDelete
)

func (session *Session) canViewRemoteLogs() bool {
	return session.remoteLogHost() &&
		session.conn != nil && !session.disconnected && session.phase == sshconn.PhaseConnected
}

func (session *Session) stopRemoteLogs() {
	if view := session.remoteLogs; view != nil && view.cancel != nil {
		view.cancel()
		view.cancel = nil
	}
}

func (session *Session) clearRemoteLog() {
	session.stopRemoteLogs()
	if view := session.remoteLogs; view != nil {
		view.selected, view.buffer, view.snapshot = remoteLogPath{}, nil, remoteLogSnapshot{}
	}
}

func (state *State) followRemoteLog(session *Session, view *remoteLogView, path remoteLogPath) {
	if session.remoteLogs != view || !session.canViewRemoteLogs() || !path.valid() {
		return
	}
	session.stopRemoteLogs()
	ctx, cancel := context.WithCancel(context.Background())
	buffer := new(remoteLogBuffer)
	view.cancel, view.buffer, view.selected = cancel, buffer, path
	view.snapshot = remoteLogSnapshot{}
	view.matchQuery, view.matchRevision, view.matches = "", 0, view.matches[:0]
	view.follow, view.top, view.scrollRemainder = true, 0, 0
	conn, notify := session.conn, state.notifyFor(session)
	go func() {
		first := true
		stream := conn.StreamCommand
		if tolerant, ok := conn.(interface {
			StreamCommandSkippingLongLines(context.Context, string, int, func([]byte) bool) error
		}); ok {
			stream = tolerant.StreamCommandSkippingLongLines
		}
		err := stream(ctx, remoteLogCommand(path.Path), remoteLogLineLimit, func(line []byte) bool {
			if ctx.Err() != nil {
				return false
			}
			if first {
				first = false
				if string(line) == "\n" {
					buffer.mu.Lock()
					buffer.ready = true
					buffer.revision++
					buffer.mu.Unlock()
					notify()
					return true
				}
			}
			if buffer.append(line) {
				notify()
			}
			return true
		})
		buffer.mu.Lock()
		buffer.done, buffer.err = true, err
		buffer.revision++
		buffer.mu.Unlock()
		if ctx.Err() == nil {
			notify()
		}
		cancel()
	}()
}

func (state *State) saveRemoteLogPath(session *Session, original, replacement remoteLogPath, deleting bool) error {
	index := state.hostIndex(session.HostID)
	if index < 0 {
		return errors.New(state.messages.HostSaveMissing)
	}
	previous := state.hosts[index]
	if !session.canViewRemoteLogs() {
		return errors.New(state.messages.RemoteLogs.Disconnected)
	}
	paths := slices.Clone(previous.RemoteLogPaths)
	entry := slices.Index(paths, original)
	m := state.messages.RemoteLogs
	if original != (remoteLogPath{}) && entry < 0 {
		return errors.New(m.PathChanged)
	}
	if deleting {
		if entry < 0 {
			return errors.New(m.PathChanged)
		}
		paths = slices.Delete(paths, entry, entry+1)
	} else {
		if !replacement.valid() {
			return errors.New(m.InvalidPath)
		}
		duplicate := slices.IndexFunc(paths, func(p remoteLogPath) bool { return p.Path == replacement.Path })
		if duplicate >= 0 && duplicate != entry {
			return errors.New(m.DuplicatePath)
		}
		if entry >= 0 {
			paths[entry] = replacement
		} else {
			if len(paths) >= maxRemoteLogPaths {
				return errors.New(m.PathLimit)
			}
			paths = append(paths, replacement)
		}
	}
	updated := previous
	updated.RemoteLogPaths = paths

	state.replaceHost(index, previous, updated)
	state.hostDataChanged()
	if err := state.saveWithResultWithoutRefresh(); err != nil {
		state.replaceHost(index, updated, previous)
		state.hostDataChanged()
		return err
	}
	return nil
}
