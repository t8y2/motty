package workbench

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"motty/internal/cmdhistory"
	"motty/internal/completion"
	"motty/internal/localshell"
	"motty/internal/pathcomp"
	"motty/internal/sftpclient"
)

// Path completion: the files of the shell's own machine offered where a
// command line names one. The grammar and the escaping live in pathcomp; this
// file owns where the listing comes from and keeps every read off the window
// thread.
//
// Ownership: the session's filesystem is the application's (SFTP for a server,
// this computer's for a local terminal), and so is what a completion types into
// the shell. The popover is Viva's, used as the command list already uses it.

const (
	// pathCacheDirs bounds the directories one session remembers.
	pathCacheDirs = 16
	// pathNameEntries bounds one listing kept for completion. The worker limits
	// a names-only listing to the same order of magnitude.
	pathNameEntries = 2048
	// pathLocalLimit bounds how many local entries are read before sorting; a
	// larger folder is not worth walking on every prompt.
	pathLocalLimit = 20000
	// pathLocalLinks bounds the local symbolic links that are followed to find
	// out whether they name a folder.
	pathLocalLinks = 200
)

// pathCompletion is one session's directory cache and, remotely, its own SFTP
// worker. It is never the file panel's worker: completion must not depend on
// the panel being open, and a folder listing for a menu must not queue behind a
// transfer. All fields except the worker are touched only on the window thread.
type pathCompletion struct {
	worker     *sftpclient.Worker
	localReady chan pathLocalResult
	done       chan struct{}
	closeOnce  sync.Once
	generation uint64

	cache    map[string]*pathListing
	order    []string
	inflight map[string]bool
	pending  map[string]uint64 // input guard epoch each in-flight read was asked for

	local  bool
	distro string // a WSL distribution a local shell runs in, or ""
}

type pathListing struct {
	entries []pathcomp.Entry
	err     error
	fetched bool
	epoch   uint64
}

type pathLocalResult struct {
	dir        string
	entries    []pathcomp.Entry
	err        error
	epoch      uint64
	generation uint64
}

// completionPathTarget is the directory a line's current word reads and what
// the rows show in front of each name.
type completionPathTarget struct {
	dir     string // absolute directory to read
	base    string // the typed final component, escapes removed
	display string // the typed directory text, escapes removed
	key     string // command + typed directory, for dismissal
}

// pathCompletionFor is the session's own cache and reader, created on first
// use. It returns nil when the session cannot read files.
func (state *State) pathCompletionFor(session *Session) *pathCompletion {
	if session == nil {
		return nil
	}
	if session.pathCompletion != nil {
		return session.pathCompletion
	}
	pc := &pathCompletion{
		cache:    make(map[string]*pathListing),
		inflight: make(map[string]bool),
		pending:  make(map[string]uint64),
		done:     make(chan struct{}),
		local:    session.isLocal(),
	}
	if pc.local {
		pc.distro = session.wslDistro()
		pc.localReady = make(chan pathLocalResult, 8)
	} else {
		if session.conn == nil || !session.hasServer() {
			return nil
		}
		conn, transfer := session.conn, session.host.Transfer
		pc.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) {
			return openRemoteFiles(conn, transfer)
		}, state.notifyFor(session))
	}
	session.pathCompletion = pc
	return pc
}

// closeCompletionPath releases a session's reader and forgets its listings.
func (state *State) closeCompletionPath(session *Session) {
	if session == nil {
		return
	}
	pc := session.pathCompletion
	if pc == nil {
		return
	}
	session.pathCompletion = nil
	pc.closeOnce.Do(func() {
		pc.generation++
		close(pc.done)
		if pc.worker != nil {
			pc.worker.Close()
		}
	})
	pc.cache = nil
	pc.inflight = nil
	pc.pending = nil
	if state.completion.list.active && state.completion.list.sessionID == session.ID {
		state.closeCompletionList()
	}
}

// completionPathDirectory reports where the session's shell is and what it
// calls home, for the paths a completion may read. Only a trusted prompt report
// is used, the same gate the file panel follows.
func (state *State) completionPathDirectory(session *Session) (cwd, home string, ok bool) {
	if session == nil || session.term == nil || session.containerCommand != "" {
		return "", "", false
	}
	t := session.term
	// The directory is the last one a trusted prompt report left behind. Typing
	// clears promptStarted, so it cannot be required here; the report for the
	// prompt the input guard proves is delivered before that prompt's end.
	if !t.folderIntegrationSeen {
		return "", "", false
	}
	// A nested shell does not report its directory, so the last report belongs
	// to the parent shell: a relative path read from it could name a file that
	// is not there. Command completion still works there (#783).
	if state.completionScreenPrompt(session) {
		return "", "", false
	}
	distro := ""
	if session.isLocal() {
		dir := session.localDirectory
		if !validLocalDirectory(dir) {
			return "", "", false
		}
		distro = session.wslDistro()
		if distro != "" {
			linux, ok := localshell.WSLPath(dir, distro)
			if !ok {
				return "", "", false
			}
			dir = linux
		}
		home = t.reportedHome
		if home != "" && distro != "" {
			if linuxHome, ok := localshell.WSLPath(home, distro); ok {
				home = linuxHome
			} else {
				home = ""
			}
		}
		if home == "" && distro == "" {
			home, _ = os.UserHomeDir()
		}
		if !path.IsAbs(dir) || pathHasControl(dir) {
			return "", "", false
		}
		return dir, home, true
	}
	if !session.hasServer() || session.conn == nil {
		return "", "", false
	}
	if !validSyncDirectory(session.startDirectory) {
		return "", "", false
	}
	return session.startDirectory, t.reportedHome, true
}

// completionPathTarget resolves the query against the shell's directory. It
// returns false when the path cannot be named in a form the session can read.
func (state *State) completionPathTarget(session *Session, ctx pathcomp.Context) (completionPathTarget, bool) {
	cwd, home, ok := state.completionPathDirectory(session)
	if !ok {
		return completionPathTarget{}, false
	}
	var dir string
	switch {
	case ctx.Home:
		if home == "" {
			return completionPathTarget{}, false
		}
		dir = path.Join(home, ctx.Query)
	case strings.HasPrefix(ctx.Query, "/"):
		dir = ctx.Query
	case ctx.Query != "":
		dir = path.Join(cwd, ctx.Query)
	default:
		dir = cwd
	}
	dir = path.Clean(dir)
	if !path.IsAbs(dir) || len(dir) > pathcomp.MaxLineBytes || pathHasControl(dir) {
		return completionPathTarget{}, false
	}
	return completionPathTarget{dir: dir, base: ctx.Base, display: ctx.Dir, key: ctx.Command + "\x00" + ctx.Dir}, true
}

func pathHasControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// completionPathCandidates answers the path position of line. loading reports
// that its directory is being read, so the caller can say so rather than
// showing an empty list.
func (state *State) completionPathCandidates(session *Session, line string) (candidates []completion.Candidate, loading bool) {
	ctx, ok := pathcomp.Parse(line)
	if !ok || !ctx.Path || cmdhistory.Secret(line) {
		return nil, false
	}
	target, ok := state.completionPathTarget(session, ctx)
	if !ok {
		return nil, false
	}
	pc := state.pathCompletionFor(session)
	if pc == nil {
		return nil, false
	}
	listing, loading := state.pathListingFor(pc, session, target)
	if listing.err != nil || !listing.fetched {
		return nil, loading
	}
	rows := pathcomp.Candidates(ctx, listing.entries)
	out := make([]completion.Candidate, len(rows))
	for index, row := range rows {
		out[index] = completion.Candidate{Text: row.Text, Insert: row.Insert, Kind: completion.KindPath, Dir: row.Dir}
	}
	return out, false
}

// pathListingFor returns the cached listing for a target and asks for a read
// when there is none, or when a new prompt may have changed the directory.
func (state *State) pathListingFor(pc *pathCompletion, session *Session, target completionPathTarget) (*pathListing, bool) {
	listing := pc.cache[target.dir]
	if listing == nil {
		listing = &pathListing{}
		pc.cache[target.dir] = listing
		pc.order = append(pc.order, target.dir)
		state.trimPathCache(pc)
	} else {
		state.touchPathCache(pc, target.dir)
	}
	// The input guard's epoch changes with each prompt, not with each key, so a
	// line's directory is read once and refreshed when the next prompt arrives.
	epoch := session.term.inputGuard.epoch
	if !pc.inflight[target.dir] && (!listing.fetched || listing.epoch != epoch) {
		if state.requestPathListing(pc, target.dir, epoch) {
			pc.inflight[target.dir] = true
			pc.pending[target.dir] = epoch
		}
	}
	return listing, !listing.fetched
}

// requestPathListing asks for one directory read. A remote read goes through
// the session's own names-only worker; a local one runs in a goroutine, so a
// slow network mount cannot stall the window.
func (state *State) requestPathListing(pc *pathCompletion, dir string, epoch uint64) bool {
	if pc.worker != nil {
		return pc.worker.ListNames(dir)
	}
	if pc.localReady == nil {
		return false
	}
	generation, distro := pc.generation, pc.distro
	go func() {
		entries, err := readLocalPathDirectory(dir, distro)
		select {
		case pc.localReady <- pathLocalResult{dir: dir, entries: entries, err: err, epoch: epoch, generation: generation}:
		case <-pc.done:
		}
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}()
	return true
}

// touchPathCache makes dir the most recently used one.
func (state *State) touchPathCache(pc *pathCompletion, dir string) {
	for index, entry := range pc.order {
		if entry != dir {
			continue
		}
		if index != len(pc.order)-1 {
			pc.order = append(append(pc.order[:index], pc.order[index+1:]...), dir)
		}
		return
	}
}

// trimPathCache forgets the least recently used directory, never one whose read
// is still in flight.
func (state *State) trimPathCache(pc *pathCompletion) {
	for len(pc.order) > pathCacheDirs {
		evicted := false
		for index, dir := range pc.order {
			if pc.inflight[dir] {
				continue
			}
			pc.order = append(pc.order[:index], pc.order[index+1:]...)
			delete(pc.cache, dir)
			delete(pc.pending, dir)
			evicted = true
			break
		}
		if !evicted {
			return
		}
	}
}

// pumpCompletionPath moves finished reads into the cache. It runs in Wake, on
// the window thread, for every session: a background tab may have a read in
// flight, and its answer must be kept for when the tab is next shown.
func (state *State) pumpCompletionPath(session *Session) {
	pc := session.pathCompletion
	if pc == nil {
		return
	}
	changed := state.drainPathWorker(session, pc)
	if state.drainPathLocal(session, pc) {
		changed = true
	}
	if !changed || state.activeSessionID != session.ID {
		return
	}
	if state.completion.list.active && state.completion.list.sessionID == session.ID {
		state.completionRebuild(session)
		return
	}
	state.completionMaybeAuto(session)
}

func (state *State) drainPathWorker(session *Session, pc *pathCompletion) bool {
	changed := false
	for pc.worker != nil {
		select {
		case result, ok := <-pc.worker.Results():
			if !ok {
				pc.worker = nil
				return changed
			}
			if result.Operation != sftpclient.OperationList || !result.NamesOnly {
				continue
			}
			epoch := pc.pending[result.Path]
			delete(pc.pending, result.Path)
			state.recordPathListing(pc, result.Path, pathEntriesFromRemote(result.Entries), result.Err, epoch)
			changed = true
		default:
			return changed
		}
	}
	return changed
}

func (state *State) drainPathLocal(session *Session, pc *pathCompletion) bool {
	changed := false
	for {
		select {
		case result := <-pc.localReady:
			if result.generation != pc.generation {
				continue
			}
			state.recordPathListing(pc, result.dir, result.entries, result.err, result.epoch)
			changed = true
		default:
			return changed
		}
	}
}

func (state *State) recordPathListing(pc *pathCompletion, dir string, entries []pathcomp.Entry, err error, epoch uint64) {
	delete(pc.inflight, dir)
	delete(pc.pending, dir)
	listing := pc.cache[dir]
	if listing == nil {
		return // the cache forgot it while the read ran
	}
	listing.entries, listing.err, listing.fetched, listing.epoch = entries, err, true, epoch
}

// pathEntriesFromRemote keeps the names a completion can type, in the order the
// worker sorted them. Owner and link details are deliberately unused.
func pathEntriesFromRemote(entries []sftpclient.Entry) []pathcomp.Entry {
	out := make([]pathcomp.Entry, 0, min(len(entries), pathNameEntries))
	for _, entry := range entries {
		if len(out) >= pathNameEntries {
			break
		}
		if !safePathName(entry.Name) {
			continue
		}
		out = append(out, pathcomp.Entry{Name: entry.Name, Dir: entry.IsDir(), Link: entry.IsLink()})
	}
	return out
}

// readLocalPathDirectory reads one folder for a local session. A WSL shell's
// paths are translated to the distribution's Windows share; the names stay in
// the shell's own form.
func readLocalPathDirectory(dir, distro string) ([]pathcomp.Entry, error) {
	native := filepath.FromSlash(dir)
	if distro != "" {
		windows, ok := localshell.WSLWindowsPath(dir, distro)
		if !ok {
			return nil, os.ErrInvalid
		}
		native = windows
	}
	handle, err := os.Open(native)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	entries := make([]pathcomp.Entry, 0, 64)
	links := 0
	for len(entries) < pathLocalLimit {
		infos, readErr := handle.ReadDir(256)
		for _, info := range infos {
			if entry, ok := pathEntryFromInfo(native, info, &links); ok {
				entries = append(entries, entry)
			}
			if len(entries) >= pathLocalLimit {
				break
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	sortPathEntries(entries)
	if len(entries) > pathNameEntries {
		entries = entries[:pathNameEntries]
	}
	return entries, nil
}

func pathEntryFromInfo(native string, info os.DirEntry, links *int) (pathcomp.Entry, bool) {
	name := info.Name()
	if !safePathName(name) {
		return pathcomp.Entry{}, false
	}
	entry := pathcomp.Entry{Name: name, Dir: info.IsDir()}
	if info.Type()&os.ModeSymlink != 0 {
		entry.Link = true
		// Following a link costs a syscall, so only a bounded number of them
		// are followed: a folder of links must not delay the popup.
		if *links < pathLocalLinks {
			*links++
			if target, err := os.Stat(filepath.Join(native, name)); err == nil && target.IsDir() {
				entry.Dir = true
			}
		}
	}
	return entry, true
}

// sortPathEntries puts directories first, then names, so the popup's first row
// is the one a user most often wants.
func sortPathEntries(entries []pathcomp.Entry) {
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].Dir != entries[right].Dir {
			return entries[left].Dir
		}
		return entries[left].Name < entries[right].Name
	})
}

// safePathName reports a name that can be shown and typed: valid UTF-8 with no
// control characters. A server is free to return anything.
func safePathName(name string) bool {
	return name != "" && name != "." && name != ".." && utf8.ValidString(name) &&
		strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}
