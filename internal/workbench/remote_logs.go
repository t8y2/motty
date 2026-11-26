package workbench

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"motty/internal/configstore"
	"motty/internal/sshconn"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
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
	return "{ if [ ! -f " + file + " ]; then " +
		// `test -f` is also false when a directory above the file cannot be
		// searched, which a log the account may not read looks like. Walking up
		// to the first directory that exists separates that from a missing
		// path, so the two are not reported as the same failure.
		"dir=" + file + "; while [ ! -d \"$dir\" ] && [ \"$dir\" != / ]; do dir=${dir%/*}; [ -z \"$dir\" ] && dir=/; done; " +
		"if [ ! -x \"$dir\" ]; then printf '%s\\n' 'Permission denied.'; exit 1; fi; " +
		"printf '%s\\n' 'Log path is missing or is not a regular file.'; exit 1; fi; " +
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
	// prefix counts the bytes of the lines the ring already dropped, each with
	// the newline that separated it. Selection offsets are byte offsets of the
	// retained text with this prefix added, so they survive a rotation.
	prefix   int64
	revision uint64
	ready    bool
	done     bool
	err      error
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
		b.prefix += int64(len(b.lines[index])) + 1
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
	prefix   int64
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
	s.revision, s.first, s.prefix, s.ready, s.done, s.err = b.revision, b.first, b.prefix, b.ready, b.done, b.err
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
	name               ui.Field
	path               text.Editor
	formMode           remoteLogForm
	formOriginal       remoteLogPath
	formError          string
	formErrorSelection ui.TextSelectionState // what the user selected of formError, to copy it
	follow             bool
	height             float32
	expanded, resizing bool
	pointerOffset      float32
	matches            []int
	matchQuery         string
	matchRevision      uint64
	// scroll is the page's area for the output; viewport is its height as of
	// the last paint, and lastOffset/lastFollow keep the auto-scroll switch in
	// step with a wheel or a drag. resetScroll asks the next declaration to go
	// back to the top, as a new search does.
	scroll      *ui.ScrollState
	viewport    float32
	lastOffset  float32
	lastFollow  bool
	resetScroll bool

	// The output is read as wrapped rows of the retained lines, one row per
	// wrapped piece, so a long line is never cut off at the panel's edge.
	// rows indexes snapshot.lines; lineStarts is where each retained line
	// begins in the same byte space, view.selection addresses it, and the wrap
	// ring remembers where each absolute line broke at the current width.
	rows         []remoteLogRow
	rowsRevision uint64
	rowsCols     int
	rowsQuery    string
	rowsReady    bool
	lineStarts   []int64
	plainStart   int64
	plainEnd     int64
	wrapCols     int
	wrap         []remoteLogLineWrap
	selection    ui.TextSelection
	// textOrigin and cellWidth are the text's last painted origin and cell
	// width; a pointer press is mapped to a byte offset through them.
	textOrigin gfx.Point
	cellWidth  float32
}

// remoteLogRow is one painted row of a line: the line's index in snapshot.lines
// and the byte range of it the row shows. A wrapped line has several rows.
type remoteLogRow struct {
	line       int32
	start, end int32
}

// remoteLogLineWrap is where one absolute line broke at the current width.
// ends[i] is the byte offset just past row i, so row i begins at ends[i-1].
type remoteLogLineWrap struct {
	abs  int64
	ends []int32
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
		view.selected, view.buffer = remoteLogPath{}, nil
		view.resetOutput()
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
	view.resetOutput()
	view.matchQuery, view.matchRevision, view.matches = "", 0, view.matches[:0]
	view.follow, view.lastFollow, view.lastOffset, view.resetScroll = true, true, 0, true
	// The stream runs where the session runs its other remote commands, so the
	// sudo privilege the tools and the monitor use also reaches a log the
	// account itself cannot read.
	runner, notify := commandRunnerFor(session), state.notifyFor(session)
	go func() {
		first := true
		stream := runner.StreamCommand
		if tolerant, ok := runner.(interface {
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
	// This save is not under another store's lock, so it may flush the sync. A
	// conflict is returned rather than retried here: the retry would rebuild the
	// snapshot from the store and silently drop the path just added. Reloading
	// the shared records after the save returns (the same order as host edits)
	// gives the next attempt a current base.
	if err := state.saveWithResultModeRecovery(true, false, true); err != nil {
		if errors.Is(err, configstore.ErrConflict) {
			refreshErr := state.refreshShared()
			if refreshErr == nil {
				// The reload restored the stored host, so the failed edit must not
				// be rolled back onto whatever now sits at this index.
				return err
			}
			err = errors.Join(err, fmt.Errorf("reload configuration after conflict: %w", refreshErr))
		}
		state.replaceHost(index, updated, previous)
		state.hostDataChanged()
		return err
	}
	return nil
}
