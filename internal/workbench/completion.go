package workbench

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
	"motty/internal/completion"
	"motty/internal/i18n"
	"motty/internal/pathcomp"
)

// Command completion: the offline command/option catalog next to the command
// history, offered in a list the user asks for and as inline ghost text.
//
// Ownership: the catalog and its matcher are UI-free (package completion); what
// a terminal shows and what bytes reach the shell stay the application's. The
// list itself is a Viva popover painted over the grid, anchored to the cursor.
//
// The catalog is data: nothing in it is executed and no completion runs a
// command, looks at files, or asks the shell what it supports. History is read
// only while the learning switch is on; the catalog works without it.

// completionHits is the place of the controls this file paints.
var completionHits = tree.Feature("command-completion")

var (
	hitCompletionOpen    = tree.ID(completionHits, "completion-open")
	hitCompletionDismiss = tree.ID(completionHits, "completion-dismiss")
	hitCompletionList    = tree.Sub(completionHits, tree.Name("completion-list"))
	hitCompletionUpdate  = tree.ID(completionHits, "completion-update")

	hitSettingsCompletionOff  = tree.ID(settingsHits, "settings-completion-off")
	hitSettingsCompletionList = tree.ID(settingsHits, "settings-completion-list")
	hitSettingsCompletionBoth = tree.ID(settingsHits, "settings-completion-both")
	hitSettingsCompletionHint = tree.ID(settingsHits, "settings-completion-hint")
)

// completionMode is how much of command completion is on. The settings file
// keeps it by name; the two older preferences are still written for a build
// that predates the name.
type completionMode uint8

const (
	completionOff    completionMode = iota // no automatic list and no inline suggestion
	completionList                         // the list opens as the user types
	completionInline                       // only the gray inline suggestion
)

func (state *State) completionMode() completionMode {
	switch {
	case state.history.suggestOff:
		return completionOff
	case state.history.inlineOff:
		return completionList
	default:
		return completionInline
	}
}

// setCompletionMode reports whether the mode changed. An automatic mode starts
// the data it needs here, in answer to the user; Off loads it only when the
// shortcut asks for the list.
func (state *State) setCompletionMode(mode completionMode) bool {
	if state.completionMode() == mode {
		return false
	}
	state.history.suggestOff = mode == completionOff
	state.history.inlineOff = mode != completionInline
	if mode == completionOff {
		state.closeCompletionList()
		state.forgetCompletionHistory()
		return true
	}
	state.loadCompletionCatalog()
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && state.history.on {
		state.completionHistory(session)
	}
	return true
}

var settingsCompletionChoices = [...]completionMode{completionOff, completionList, completionInline}

// persistName is how a mode is written to the settings file.
func (mode completionMode) persistName() string {
	switch mode {
	case completionOff:
		return "off"
	case completionList:
		return "list"
	}
	return "inline"
}

func (state *State) settingsCompletionLabel(mode completionMode) string {
	m := state.messages.Completion
	switch mode {
	case completionOff:
		return m.ModeOff
	case completionList:
		return m.ModeList
	}
	return m.ModeInline
}

// completionScope names the popover's focus scope; it is separate from the
// terminal's so the grid keeps the keyboard and the IME.
var completionScope = tree.Sub(completionHits, tree.Name("completion-scope"))

const (
	// completionRows is how many candidates the popover lists at once; the rest
	// scroll with the arrow keys.
	completionRows = 10
	// completionHistoryRows caps how many history matches join the host's
	// commands in the popover.
	completionHistoryRows = 5
	// completionRowHeight is one candidate row.
	completionRowHeight = float32(24)
	// The width follows the widest row but never drops below the menus' own
	// minimum, and leaves room for the leading icon before the text.
	completionMinWidth  = float32(144)
	completionMaxWidth  = float32(560)
	completionIconSize  = float32(14)
	completionIconInset = float32(6)
	completionIconGap   = float32(7)
	completionTextEnd   = float32(8)
	completionTextInset = float32(8)
	// completionTextStart is where a row's text begins when the list has path
	// rows and therefore a glyph column.
	completionTextStart = completionIconInset + completionIconSize + completionIconGap
)

// completionUI is the catalog, its loads, and the open list.
type completionUI struct {
	ctx            context.Context
	cancel         context.CancelFunc
	workers        sync.WaitGroup
	catalog        *completion.Catalog
	catalogErr     error
	catalogNote    error
	store          *completion.CatalogStore
	catalogLoading bool
	catalogReady   chan completionCatalogLoad
	// catalogGeneration belongs to the per-directory store; another window
	// can notice an install without reading files.
	catalogGeneration uint64

	// updating is a user-requested download. Its result arrives through
	// updateReady; checked is when the feed was last asked.
	updating bool
	// updatePending is a check asked for before the catalog it is compared
	// against had loaded; it starts when the load finishes.
	updatePending bool
	updateErr     error
	// updateStats belongs to the catalog this window holds; it is adopted
	// together with the catalog from the shared store, so every window shows
	// the counters of the same generation.
	updateStats  completion.ImportStats
	checked      time.Time
	updateReady  chan completionUpdateResult
	cancelUpdate context.CancelFunc

	// History reads fill history.ghost, the single cache used by both modes.
	// The catalog never waits for these background requests.
	historyPending map[string]bool
	historyReady   chan completionHistoryLoad
	// generation invalidates loads started before the history changed under
	// them: a cleared or newly disabled history must not be refilled by an
	// answer to an old question.
	generation uint64

	// autoHold suppresses the automatic path menu after a candidate was
	// accepted, until the user types again. Without it the echo of the accepted
	// suffix would reopen the menu and swallow the Enter that runs the line.
	autoHold completionAutoHold
	// accepting covers the synchronous revalidation and drain of an acceptance,
	// which reads output before the hold is set.
	accepting bool
	// manual is set while a list the shortcut opened under Off is on screen: the
	// data it needs is loaded then, although the automatic modes load it at start.
	manual bool
	// dismissed remembers the path context the user closed with Escape.
	dismissed completionDismiss

	list completionPopup
}

// completionAutoHold is the session whose accepted path keeps the automatic
// menu closed until its next input.
type completionAutoHold struct{ session uint32 }

// completionDismiss is one Escape: the prompt generation and the path context
// it closed. Typing more of the same argument does not reopen the menu; a new
// prompt or a different command and directory does.
type completionDismiss struct {
	epoch uint64
	key   string
}

type completionCatalogLoad struct {
	err  error
	note error
}

type completionUpdateResult struct {
	err error
	// installed is the store's own answer to "was a new snapshot written".
	// The catalog generation cannot answer it here: a Wake may have adopted
	// the published catalog before the result was handled.
	installed bool
}

type completionHistoryLoad struct {
	key        string
	entries    []cmdhistory.Entry
	generation uint64
}

// completionPopup is the assembled list. Everything is a snapshot: accepting
// revalidates all of it against the session before anything is sent.
type completionPopup struct {
	active bool
	// auto marks the list opened by typing, not by the shortcut. Only an
	// automatic list follows the line as it is typed and closes on Escape until
	// its context changes.
	auto bool
	// loading marks a list whose directory is still being read.
	loading bool

	suggestionSnapshot

	candidates []completion.Candidate
	// selected is -1 until the arrows choose a row. An automatic list starts
	// with nothing highlighted: a menu that pops up while typing must not look
	// as if a choice was already made.
	selected int
	top      int
}

// completionCatalog only reads a prepared snapshot. Wake owns background work;
// painting and ghost lookup must never start I/O or goroutines.
func (state *State) completionCatalog() *completion.Catalog {
	return state.completion.catalog
}

func (state *State) loadCompletionCatalog() {
	c := &state.completion
	if c.catalog != nil || c.catalogErr != nil || c.catalogLoading {
		return
	}
	if c.store == nil {
		c.store = state.completionStore()
	}
	c.catalogLoading = true
	ready, store, invalidate := make(chan completionCatalogLoad, 1), c.store, state.options.Invalidate
	ctx := state.completionContext()
	c.workers.Add(1)
	c.catalogReady = ready
	go func() {
		defer c.workers.Done()
		_, _, note, err := store.LoadContext(ctx)
		ready <- completionCatalogLoad{note: note, err: err}
		if invalidate != nil {
			invalidate()
		}
	}()
}

func (state *State) completionStore() *completion.CatalogStore {
	path := completion.CatalogPath(state.options.StatePath)
	group := state.options.Group
	if group == nil {
		return completion.NewCatalogStore(path)
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	if group.completionStores == nil {
		group.completionStores = make(map[string]*completion.CatalogStore)
	}
	if group.completionStores[path] == nil {
		group.completionStores[path] = completion.NewCatalogStore(path)
	}
	return group.completionStores[path]
}

// adoptCompletion runs only on the UI thread. Every window reads the same
// per-directory store; a late load result cannot publish over a newer install.
func (state *State) adoptCompletion() {
	// With completion off and no manual list nothing needs the catalog, so it
	// is not decoded. A decoded catalog keeps only its root commands until one
	// is used.
	if state.history.suggestOff && !state.completion.manual && !state.completion.updatePending {
		state.adoptCompletionUpdate()
		state.adoptCompletionHistory()
		return
	}
	state.loadCompletionCatalog()
	state.adoptCompletionCatalog()
	state.adoptCompletionUpdate()
	state.adoptCompletionHistory()
	if state.history.on && !state.historyClearing() {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && state.historyAvailable(session) && !session.host.NoHistory {
			state.completionHistory(session)
			// The merged history of every host feeds the inline suggestion's
			// fallback, so it is requested here beside the host's own: a frame
			// only reads what has landed (#219).
			if !state.history.inlineOff {
				state.suggestionHistory("")
			}
		}
	}
}

func (state *State) adoptCompletionCatalog() {
	c := &state.completion
	if c.catalogReady != nil {
		select {
		case load := <-c.catalogReady:
			c.catalogReady = nil
			c.catalogLoading = false
			c.catalogErr, c.catalogNote = load.err, load.note
			if load.err != nil {
				c.updatePending = false
			}
		default:
		}
	}
	if c.store == nil {
		return
	}
	catalog, generation, stats := c.store.Snapshot()
	if catalog != nil && generation != c.catalogGeneration {
		c.catalog, c.catalogGeneration, c.updateStats = catalog, generation, stats
		state.refreshOpenCompletion()
	}
	if c.catalog != nil && c.updatePending && !c.updating {
		c.updatePending = false
		state.updateCompletions()
	}
}

func (state *State) adoptCompletionUpdate() {
	c := &state.completion
	if c.updateReady == nil {
		return
	}
	select {
	case result := <-c.updateReady:
		c.updateReady = nil
		c.updating, c.checked = false, time.Now()
		if c.cancelUpdate != nil {
			c.cancelUpdate()
			c.cancelUpdate = nil
		}
		c.updateErr = result.err
		state.adoptCompletionCatalog()
		installed := result.err == nil && result.installed && c.catalog != nil
		if installed {
			// The store keeps the counters beside the catalog, so
			// adoptCompletionCatalog already adopted them for every window.
			// Compose the one-shot message from that adopted snapshot, not
			// from the result: another window may have installed a newer
			// release while this result waited, and the status must not claim
			// that newer release needed repairs it does not have.
			c.catalogNote = nil
			message := state.messages.Completion
			if c.updateStats.RepairedFiles > 0 || c.updateStats.SkippedSources > 0 {
				state.status = completionStatusText(fmt.Sprintf(message.UpdatedRepairedFmt, c.catalog.Release, c.updateStats.RepairedFiles, c.updateStats.SkippedSources))
			} else {
				state.status = completionStatusText(fmt.Sprintf(message.UpdatedFmt, c.catalog.Release))
			}
		} else if result.err == nil && c.catalog != nil {
			state.status = state.messages.Completion.UpToDate
		}
	default:
	}
}

func (state *State) adoptCompletionHistory() {
	c := &state.completion
	for c.historyReady != nil {
		select {
		case load := <-c.historyReady:
			delete(c.historyPending, load.key)
			if load.generation != c.generation {
				continue
			}
			if state.history.ghost == nil {
				state.history.ghost = make(map[string][]cmdhistory.Entry)
			}
			state.history.ghost[load.key] = load.entries
			state.closeCompletionList()
		default:
			return
		}
	}
}

// completionHistory is shared by the list and ghost text. Reads are requested
// by Wake/input handlers, while painting reads this cache without doing I/O.
func (state *State) completionHistory(session *Session) []cmdhistory.Entry {
	if state.history.suggestOff && !state.completion.manual {
		return nil
	}
	if !state.history.on || state.historyClearing() || !state.historyAvailable(session) || session.host.NoHistory {
		return nil
	}
	return state.suggestionHistory(historyKey(session))
}

// suggestionHistory is the cached candidates of one history key — a host's
// own, or the merged history of every host under the empty key. A missing one
// is read in the background and lands by a later Wake.
func (state *State) suggestionHistory(key string) []cmdhistory.Entry {
	if entries, ok := state.history.ghost[key]; ok {
		return entries
	}
	c := &state.completion
	if c.historyPending == nil {
		c.historyPending = make(map[string]bool)
	}
	if c.historyPending[key] {
		return nil
	}
	store, path := state.history.store, state.historyPath()
	if c.historyReady == nil {
		c.historyReady = make(chan completionHistoryLoad, 128)
	}
	c.historyPending[key] = true
	generation, ready, invalidate := c.generation, c.historyReady, state.options.Invalidate
	ctx := state.completionContext()
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		entries := readSuggestionHistory(store, path, key)
		// Keep one outstanding read per host even after invalidation. The
		// bounded inbox uses backpressure; cancellation releases closing windows.
		select {
		case ready <- completionHistoryLoad{key: key, entries: entries, generation: generation}:
		case <-ctx.Done():
			return
		}
		if invalidate != nil {
			invalidate()
		}
	}()
	return nil
}

func (state *State) forgetCompletionHistory() {
	state.completion.generation++
	state.history.ghost = nil
}

// updateCompletions only starts once the baseline is available. The shared
// store serializes download, validation and installation across all windows.
func (state *State) updateCompletions() {
	c := &state.completion
	if c.updating {
		return
	}
	if completion.CatalogPath(state.options.StatePath) == "" {
		c.updateErr = fmt.Errorf("%s", state.messages.Completion.NoDataDir)
		return
	}
	if c.catalog == nil || c.store == nil {
		c.updatePending = true
		state.loadCompletionCatalog()
		return
	}
	ctx, cancel := context.WithCancel(state.completionContext())
	c.updating, c.updateErr, c.cancelUpdate = true, nil, cancel
	ready, store := make(chan completionUpdateResult, 1), c.store
	c.workers.Add(1)
	c.updateReady = ready
	peers := []*State{state}
	if group := state.options.Group; group != nil {
		group.mu.Lock()
		peers = append([]*State(nil), group.members...)
		group.mu.Unlock()
	}
	wake := make([]func(), 0, len(peers))
	for _, peer := range peers {
		if completion.CatalogPath(peer.options.StatePath) == completion.CatalogPath(state.options.StatePath) && peer.options.Invalidate != nil {
			wake = append(wake, peer.options.Invalidate)
		}
	}
	go func() {
		defer c.workers.Done()
		_, _, _, installed, err := store.Update(ctx, completion.UpdateClient())
		ready <- completionUpdateResult{err: err, installed: installed}
		for _, invalidate := range wake {
			invalidate()
		}
	}()
}

// completionCatalogStatus is the settings description of the installed catalog:
// its real version and counts, or what the last attempt said.
func (state *State) completionCatalogStatus() (string, error) {
	c := &state.completion
	m := state.messages.Completion
	if c.updating {
		return m.Updating, nil
	}
	if c.updateErr != nil {
		return completionStatusText(fmt.Sprintf(m.CheckFailedFmt, completionStatusText(c.updateErr.Error()))), c.updateErr
	}
	if c.catalog == nil {
		if c.catalogErr != nil {
			return completionStatusText(fmt.Sprintf(m.CheckFailedFmt, completionStatusText(c.catalogErr.Error()))), c.catalogErr
		}
		if state.history.suggestOff && !c.updatePending {
			return m.CatalogOff, nil
		}
		return m.CatalogLoading, nil
	}
	status := fmt.Sprintf(m.CatalogDescriptionFmt, c.catalog.Release, c.catalog.RootCount(), c.catalog.FlagCount())
	// A full commit hash is the only provenance worth showing: a cache or a
	// test catalog with a placeholder commit falls back to the plain version.
	// The repository is a constant, never catalog data, so a tampered cache
	// cannot label itself as another project.
	if validCommit(c.catalog.Commit) {
		origin := m.CatalogOriginInstalled
		if builtin, err := completion.Builtin(); err == nil && c.catalog == builtin {
			origin = m.CatalogOriginBuiltin
		}
		status = fmt.Sprintf(m.CatalogProvenanceFmt, c.catalog.Release,
			fmt.Sprintf(m.CatalogRepoFmt, completion.RepositoryOwner+"/"+completion.RepositoryName, c.catalog.Commit[:12]),
			c.catalog.RootCount(), c.catalog.FlagCount(), origin)
	}
	if warnings := c.catalog.AuditWarnings(); warnings > 0 {
		status += " · " + fmt.Sprintf(m.AuditWarningsFmt, warnings)
	}
	if c.catalogNote != nil {
		status = fmt.Sprintf(m.CacheFailedFmt, status, completionStatusText(c.catalogNote.Error()))
	}
	if c.updateStats.RepairedFiles > 0 || c.updateStats.SkippedSources > 0 {
		status += " · " + fmt.Sprintf(m.RepairSummaryFmt, c.updateStats.RepairedFiles, c.updateStats.SkippedSources)
	}
	if _, known := c.catalog.ImportStats(); !known {
		status += " · " + m.RecoveryStatsUnknown
	}
	if !c.checked.IsZero() {
		status = fmt.Sprintf(m.CheckedFmt, status, c.checked.Format("2006-01-02 15:04"))
	}
	return completionStatusText(status), nil
}

// refreshOpenCompletion rebuilds a list that was assembled before a newly
// adopted catalog arrived, so its rows show the data that is now loaded.
func (state *State) refreshOpenCompletion() {
	if !state.completion.list.active {
		return
	}
	if session, ok := state.sessionByIDPointer(state.completion.list.sessionID); ok {
		state.completionRebuild(session)
	}
}

// completionStatusRunes bounds every catalog status string that reaches the
// settings page. A malformed archive can make the YAML parser aggregate
// megabytes of error text; the row must stay a row.
const completionStatusRunes = 240

// completionStatusText sanitizes untrusted status and error text before it is
// stored or painted: invisible characters and line breaks are removed and the
// result is bounded.
func completionStatusText(text string) string {
	return completion.NormalizeText(text, completionStatusRunes)
}

// validCommit reports whether commit is a full lowercase hexadecimal hash, the
// form a real catalog records. Anything else is a placeholder and is not shown.
func validCommit(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, r := range commit {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// completionEligible reports whether this session may show completions now.
// The catalog is public data, but a list still needs a live prompt the client
// can prove: the shell's own marker (an integrated prompt), or, when an
// interactive child shell has taken the terminal and the markers are gone, the
// screen line the recorder read (see completionScreenPrompt).
func (state *State) completionEligible(session *Session) bool {
	if session == nil || session.term == nil || session.host.NoHistory {
		return false
	}
	if session.term.suggestionPending != nil || session.term.preedit.Composing {
		return false
	}
	if state.inputGuardAvailable(session) {
		return true
	}
	return state.completionScreenPrompt(session)
}

// completionScreenPrompt is the prompt proof left when an integrated shell has
// handed the terminal to an interactive child that does not run the startup
// hooks — sudo, su, a nested ssh (#783). No marker will come until the child
// exits, so the proof is the line the recorder read at the first key of this
// command: quiet terminal, cursor after a non-empty prompt, echo matching what
// was typed. A prompt shaped like a line editor's own keeps the list out of
// programs, which echo and draw a prompt just as a shell does.
func (state *State) completionScreenPrompt(session *Session) bool {
	t := session.term
	if !t.folderIntegrationSeen || t.inputGuard.active || !state.terminalOwnedByShell(session) {
		return false
	}
	line, prompt, ok := t.recorder.LinePrompt(cmdhistory.TermScreen{Term: t.screen})
	if !ok || strings.TrimSpace(line) == "" {
		return false
	}
	return !cmdhistory.ProgramPrompt(prompt)
}

// completionFeed reports whether the recorder should follow typing so the list
// and the ghost text can see the current line. It is broader than the
// eligibility check: the recorder must keep up while a dialog or a prediction
// temporarily hides the list, and it is fed even when learning is off.
func (state *State) completionFeed(session *Session) bool {
	if session == nil || session.term == nil || session.host.NoHistory {
		return false
	}
	return state.historyAvailable(session)
}

// attachCompletion connects a terminal to the completion data and hooks.
func (state *State) attachCompletion(session *Session) {
	t := session.term
	if t == nil {
		return
	}
	sessionID := session.ID
	t.completionFeed = func() bool {
		owner, ok := state.sessionByIDPointer(sessionID)
		return ok && state.completionFeed(owner)
	}
	t.completionInput = func(input []byte) {
		if state.completion.autoHold.session == sessionID {
			state.completion.autoHold = completionAutoHold{}
		}
		list := &state.completion.list
		if !list.active || list.sessionID != sessionID {
			return
		}
		// An automatic list follows the line: a typed character filters it and
		// the echo rebuilds it. Everything an explicit list does, it still does.
		if list.auto && !completionEndsInput(input) {
			return
		}
		state.closeCompletionList()
	}
	if state.history.on {
		state.completionHistory(session)
	}
}

// completionSession is the session the list belongs to, if it still exists.
func (state *State) completionSession() *Session {
	session, ok := state.sessionByIDPointer(state.completion.list.sessionID)
	if !ok || session.term != state.completion.list.owner {
		return nil
	}
	return session
}

// openCompletions opens the list for the active terminal. Called by the
// shortcut, the command palette entry, and the mouse handler. An explicit open
// always wins over an Escape dismissal.
func (state *State) openCompletions() {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !state.completionEligible(session) {
		return
	}
	state.completion.dismissed = completionDismiss{}
	state.completion.autoHold = completionAutoHold{}
	state.closeAutofillPick()
	// Off stops the automatic list and the ghost, not the shortcut: the data it
	// needs is loaded here, the first time it is asked for.
	state.completion.manual = state.history.suggestOff
	if state.completion.manual {
		state.loadCompletionCatalog()
		state.completionHistory(session)
	}
	state.openCompletionList(session)
	if !state.completion.list.active {
		state.completion.manual = false
	}
}

// openCompletionList builds the initial candidates for a list the user asked
// for. An empty prompt or an unsupported line leaves the list closed rather
// than showing a stray frame; a path position whose directory is still being
// read opens with its loading row.
func (state *State) openCompletionList(session *Session) {
	t := session.term
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok || strings.TrimSpace(line) == "" {
		return
	}
	candidates, loading := state.completionCandidates(session, line)
	if len(candidates) == 0 && !loading {
		return
	}
	state.completion.list = completionPopup{
		active:             true,
		loading:            loading,
		suggestionSnapshot: state.captureSuggestion(session, line),
		candidates:         candidates,
	}
}

// closeCompletionList forgets the list; no data is touched.
func (state *State) closeCompletionList() {
	state.completion.list = completionPopup{}
	state.completion.manual = false
}

// dismissCompletion closes the list and, for an automatic one, remembers the
// path context so typing more of the same argument does not reopen it.
func (state *State) dismissCompletion(session *Session) {
	if state.completion.list.auto && session != nil && session.term != nil {
		if line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen}); ok {
			if ctx, ok := pathcomp.Parse(line); ok {
				key := ""
				if ctx.Path {
					if target, ok := state.completionPathTarget(session, ctx); ok {
						key = target.key
					}
				} else {
					key = commandCompletionKey(ctx)
				}
				if key != "" {
					state.completion.dismissed = completionDismiss{epoch: session.term.inputGuard.epoch, key: key}
				}
			}
		}
	}
	state.closeCompletionList()
}

// completionRebuild recomputes the candidates of the open list from the line
// the prompt shows now. It never sends input.
func (state *State) completionRebuild(session *Session) {
	list := &state.completion.list
	t := session.term
	if t == nil || !list.active || list.sessionID != session.ID || list.owner != t {
		state.closeCompletionList()
		return
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok || strings.TrimSpace(line) == "" {
		state.closeCompletionList()
		return
	}
	var candidates []completion.Candidate
	var loading bool
	if list.auto {
		// A path position keeps its own menu; every other position merges the
		// catalog and history as the shortcut's list does.
		if ctx, ok := pathcomp.Parse(line); ok && ctx.Path {
			candidates, loading = state.completionPathCandidates(session, line)
		} else {
			candidates, loading = state.completionCandidates(session, line)
		}
	} else {
		candidates, loading = state.completionCandidates(session, line)
	}
	if len(candidates) == 0 && !loading {
		state.closeCompletionList()
		return
	}
	if line != list.line {
		list.top = 0
		if list.auto {
			list.selected = -1
		} else {
			list.selected = 0
		}
	}
	list.suggestionSnapshot = state.captureSuggestion(session, line)
	list.candidates = candidates
	list.loading = loading
	if len(candidates) == 0 {
		list.selected, list.top = -1, 0
	} else {
		if list.selected >= len(candidates) {
			list.selected = len(candidates) - 1
		}
		list.clamp()
	}
}

// completionAfterOutput follows output that may have changed the prompt line:
// an open list is rebuilt, and an automatic path list may open. It does nothing
// while a list is closed and the line is not a path position.
func (state *State) completionAfterOutput(session *Session) {
	if session == nil || session.term == nil {
		return
	}
	if state.completion.list.active && state.completion.list.sessionID == session.ID {
		state.completionRefresh(session)
		return
	}
	if state.completionMode() != completionList {
		return // Off and inline-only leave the list to the shortcut
	}
	state.completionMaybeAuto(session)
}

// completionRefresh follows output that may have changed the prompt. It is
// cheap: the recorder only reports a line when the cursor is at its end.
func (state *State) completionRefresh(session *Session) {
	t := session.term
	list := &state.completion.list
	if t == nil || !list.active || list.sessionID != session.ID {
		return
	}
	if state.modal != modalNone || !state.completionEligible(session) {
		state.closeCompletionList()
		return
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok {
		state.closeCompletionList()
		return
	}
	if line == list.line && !list.loading {
		return
	}
	state.completionRebuild(session)
}

// completionMaybeAuto opens the path menu when the line ends where a shell
// reads a path and the list's directory has arrived. It waits for a directory
// rather than showing a loading row that may be gone before it is read, and it
// never opens over another menu or a program of its own.
func (state *State) completionMaybeAuto(session *Session) {
	if state.modal != modalNone || state.activeSection != sectionTerminal || state.activeSessionID != session.ID {
		return
	}
	if !state.completionEligible(session) {
		return
	}
	if state.completion.accepting || state.completion.autoHold.session == session.ID {
		return
	}
	if state.ghostMenu.open || state.terminalMenu.open || len(state.autofillRules.pick.candidates) > 0 {
		return
	}
	if _, _, _, predicting := session.term.prediction.overlay(state.now()); predicting {
		return
	}
	t := session.term
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok || strings.TrimSpace(line) == "" || cmdhistory.Secret(line) {
		return
	}
	ctx, ok := pathcomp.Parse(line)
	if !ok {
		return
	}
	if ctx.Path {
		target, ok := state.completionPathTarget(session, ctx)
		if !ok {
			return
		}
		if state.completionDismissed(t, target.key) {
			return
		}
		candidates, loading := state.completionPathCandidates(session, line)
		if loading || len(candidates) == 0 {
			return
		}
		state.completion.list = completionPopup{
			active:             true,
			auto:               true,
			selected:           -1,
			suggestionSnapshot: state.captureSuggestion(session, line),
			candidates:         candidates,
		}
		return
	}
	// The list opens for a command word and for its arguments once there is
	// more than one answer. A single answer stays the inline suggestion's job:
	// a menu over one row would repeat the ghost and hide it.
	key := commandCompletionKey(ctx)
	if state.completionDismissed(t, key) {
		return
	}
	candidates, loading := state.completionCandidates(session, line)
	if loading || len(candidates) == 0 {
		return
	}
	state.completion.list = completionPopup{
		active:             true,
		auto:               true,
		selected:           -1,
		suggestionSnapshot: state.captureSuggestion(session, line),
		candidates:         candidates,
	}
}

// completionDismissed reports whether Escape closed this context in this
// prompt. The prompt's input guard epoch scopes it to the line being typed.
func (state *State) completionDismissed(t *terminal, key string) bool {
	dismiss := state.completion.dismissed
	return dismiss.epoch == t.inputGuard.epoch && dismiss.key == key
}

// commandCompletionKey names a command line's word position for a dismissal:
// the command itself, or one of its arguments.
func commandCompletionKey(ctx pathcomp.Context) string {
	if ctx.Argument {
		return "arg\x00" + ctx.Command + "\x00" + strconv.Itoa(ctx.ArgumentIndex)
	}
	return "cmd"
}

// completionCandidates merges the paths of a path position with the host's
// history, newest first, and the catalog's stable order. Duplicates are dropped
// by what they would insert.
func (state *State) completionCandidates(session *Session, line string) ([]completion.Candidate, bool) {
	if cmdhistory.Secret(line) {
		return nil, false
	}
	pathRows, pathLoading := state.completionPathCandidates(session, line)
	candidates := make([]completion.Candidate, 0, 16+len(pathRows))
	seen := make(map[string]struct{}, 16+len(pathRows))
	add := func(candidate completion.Candidate) {
		if len(candidates) >= completion.MaxCandidates || candidate.Insert == "" {
			return
		}
		if _, ok := seen[candidate.Insert]; ok {
			return
		}
		seen[candidate.Insert] = struct{}{}
		candidates = append(candidates, candidate)
	}
	// Paths come first: where the line names a file, the user is most likely
	// choosing one, and the shell's own candidate is the shortest.
	for _, candidate := range pathRows {
		add(candidate)
	}
	if state.history.on && state.historyAvailable(session) && !session.host.NoHistory {
		// Get full commands entries from history
		fullEntries := cmdhistory.SuggestAllFull(state.completionHistory(session), line, completionHistoryRows)

		// If we have 2+ matches, calculate common prefix and add it first.
		// Exclude commands that are a prefix of another command, so the
		// common prefix is not dragged down by shorter partial commands
		// (e.g. "ip" and "ip route" should not shorten the prefix of
		// "ip route get 8.8.8.8" and "ip route get 9.9.9.9").
		if len(fullEntries) >= 2 {
			filtered := filterPrefixEntries(fullEntries)
			if len(filtered) >= 2 {
				commonPrefix := prefixWordEnd(cmdhistory.CommonPrefix(entryCommands(filtered)))
				if len(commonPrefix) > len(line) {
					insertText := commonPrefix[len(line):]
					add(completion.Candidate{Text: commonPrefix, Insert: insertText, Desc: state.messages.Completion.PrefixRow, Kind: completion.KindCommand})
				}
			}
		}

		// Add individual matches
		now := time.Now()
		for _, entry := range fullEntries {
			insertText := entry.Command[len(line):]
			add(completion.Candidate{Text: entry.Command, Insert: insertText, Desc: humanRelativeTime(now, entry.Used, state.messages.Completion), Kind: completion.KindCommand})
		}
	}
	if catalog := state.completionCatalog(); catalog != nil {
		if result := completion.Complete(catalog, line); !result.Suppressed {
			for _, candidate := range result.Candidates {
				add(candidate)
			}
		}
	}
	return candidates, pathLoading || state.completion.catalogLoading
}

// completionKey handles keys while the list is open. Printable keys stay with
// the terminal; an explicit list closes on the next typed byte, while an
// automatic one follows the line and closes on the keys that end it.
func (state *State) completionKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	list := &state.completion.list
	if !list.active {
		return gfx.EventResult{}, false
	}
	if state.modal != modalNone {
		state.closeCompletionList()
		return gfx.EventResult{}, false
	}
	session := state.completionSession()
	if session == nil || session.term.preedit.Composing {
		state.closeCompletionList()
		return gfx.EventResult{}, false
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if len(list.candidates) == 0 {
		// A loading row has nothing to choose. Enter still runs the line; the
		// other keys leave the terminal alone.
		switch event.Key {
		case gfx.KeyEscape:
			state.dismissCompletion(session)
			return handled, true
		case gfx.KeyEnter, gfx.KeyTab:
			state.closeCompletionList()
			return gfx.EventResult{}, false
		}
		return gfx.EventResult{}, false
	}
	switch event.Key {
	case gfx.KeyEscape:
		state.dismissCompletion(session)
		return handled, true
	case gfx.KeyEnter:
		if event.Repeat {
			return handled, true
		}
		if event.Modifiers != 0 {
			state.closeCompletionList()
			return gfx.EventResult{}, false
		}
		if list.auto && list.selected < 0 {
			// The automatic menu is a filter, not the line: Enter runs what was
			// typed. An arrow key chooses a row and makes Enter accept it.
			state.closeCompletionList()
			return gfx.EventResult{}, false
		}
		state.acceptCompletion(session)
		return handled, true
	case gfx.KeyTab:
		if event.Modifiers != 0 {
			return gfx.EventResult{}, false
		}
		state.closeCompletionList()
		return gfx.EventResult{}, false
	case gfx.KeyUp:
		if event.Modifiers != 0 {
			return gfx.EventResult{}, false
		}
		state.completionMove(-1)
		return handled, true
	case gfx.KeyDown:
		if event.Modifiers != 0 {
			return gfx.EventResult{}, false
		}
		state.completionMove(1)
		return handled, true
	case gfx.KeyPageUp:
		state.completionMove(-completionRows)
		return handled, true
	case gfx.KeyPageDown:
		state.completionMove(completionRows)
		return handled, true
	case gfx.KeyHome:
		list.selected, list.top = 0, 0
		return handled, true
	case gfx.KeyEnd:
		list.selected = len(list.candidates) - 1
		list.clamp()
		return handled, true
	}
	return gfx.EventResult{}, false
}

func (list *completionPopup) clamp() {
	if list.selected < 0 {
		list.top = 0
		return
	}
	if list.selected >= len(list.candidates) {
		list.selected = len(list.candidates) - 1
	}
	if list.selected < list.top {
		list.top = list.selected
	}
	if list.selected >= list.top+completionRows {
		list.top = list.selected - completionRows + 1
	}
	if top := len(list.candidates) - completionRows; list.top > top {
		list.top = max(top, 0)
	}
	if list.top < 0 {
		list.top = 0
	}
}

func (state *State) completionMove(delta int) {
	list := &state.completion.list
	if len(list.candidates) == 0 {
		return
	}
	last := len(list.candidates) - 1
	switch {
	case list.selected < 0 && delta > 0:
		list.selected = 0
	case list.selected < 0:
		list.selected = last
	case delta == 1 && list.selected == last:
		// Down past the last row arrives at the first, as the app's menus do;
		// a page step stays on the edge.
		list.selected = 0
	case delta == -1 && list.selected == 0:
		list.selected = last
	default:
		list.selected += delta
	}
	list.clamp()
}

// acceptCompletion inserts the selected suffix, and only that. Before typing it
// drains queued output and rechecks the session, terminal, prompt version and
// line the list was built from; a mismatch drops the acceptance silently.
func (state *State) acceptCompletion(session *Session) {
	list := state.completion.list
	state.closeCompletionList()
	// Revalidating drains output, and that pump may try to open the automatic
	// menu again for a line that has not changed yet.
	state.completion.accepting = true
	defer func() { state.completion.accepting = false }()
	if !list.active || list.selected < 0 || list.selected >= len(list.candidates) {
		return
	}
	session = state.suggestionOwner(list.suggestionSnapshot)
	if session == nil || !state.completionEligible(session) {
		return
	}
	candidate := list.candidates[list.selected]
	if candidate.Kind == completion.KindPath {
		state.sendPathSuggestion(session, candidate.Insert)
	} else {
		state.sendSuggestion(session, candidate.Insert, candidate.FromCatalog)
	}
	if list.auto {
		// The echo of the suffix must not reopen the menu before the next key:
		// that would put a menu under the Enter that runs the finished line.
		state.completion.autoHold = completionAutoHold{session: session.ID}
	}
}

// clickCompletion handles the list's controls.
func (state *State) clickCompletion(id gfx.ID) bool {
	if id == hitCompletionOpen {
		state.openCompletions()
		return true
	}
	if id == hitCompletionDismiss {
		if session := state.completionSession(); state.completion.list.auto && session != nil {
			state.dismissCompletion(session)
		} else {
			state.closeCompletionList()
		}
		return true
	}
	list := &state.completion.list
	if !list.active {
		return false
	}
	index, ok := id.In(hitCompletionList)
	if !ok {
		return false
	}
	if index < 0 || index >= len(list.candidates) {
		return false
	}
	list.selected = index
	if session := state.completionSession(); session != nil {
		state.acceptCompletion(session)
	}
	return true
}

// paintCompletionList draws the open list over the grid, anchored to the cursor
// cell and flipped above it when the terminal has no room below.
func (state *State) paintCompletionList(canvas *gfx.Canvas, session *Session, geometry terminalGeometry) {
	list := &state.completion.list
	if !list.active || list.sessionID != session.ID || state.modal != modalNone {
		return
	}
	if !state.completionEligible(session) || len(list.candidates) == 0 && !list.loading {
		return
	}
	cursor := session.term.presentedCursor()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	x := geometry.left + float32(cursor.X)*cellWidth
	y := geometry.top + float32(cursor.Y)*cellHeight
	area := geometry.area
	x = min(max(x, area.X+2), area.X+max(area.Width-2, 2))
	y = min(max(y, area.Y+2), area.Y+max(area.Height-2, 2))
	anchor := gfx.Rect{X: x, Y: y, Width: max(cellWidth, 2), Height: max(cellHeight, 2)}

	visible := min(len(list.candidates), completionRows)
	width := state.completionWidth(canvas)
	bodyHeight := float32(max(visible, 1))*completionRowHeight + 4
	popover := components.BeginPopover(canvas, state.Theme(), area, anchor, components.PopoverOptions{
		Layer:       dialogLayer,
		Scope:       completionScope.ID(tree.Name("popover")),
		DismissID:   hitCompletionDismiss,
		Side:        components.PopoverBottom,
		Align:       components.PopoverAlignStart,
		SideOffset:  2,
		Headerless:  true,                           // the rows are the content; a title would only take room
		Padding:     components.DropdownMenuPadding, // the rows line up with the app's menus
		Instant:     true,                           // it appears under the cursor as the user types
		ContentSize: gfx.Size{Width: width},
		BodyHeight:  bodyHeight,
	})
	layout := popover.Begin("", "")
	if len(list.candidates) == 0 {
		state.paintCompletionNote(canvas, layout.Body)
	} else {
		state.paintCompletionRows(canvas, layout.Body, visible)
	}
	popover.End()
}

// paintCompletionNote draws the one row of a list whose directory is still
// being read. It is not a candidate: nothing here can be chosen or typed.
func (state *State) paintCompletionNote(canvas *gfx.Canvas, body gfx.Rect) {
	text := gfx.Rect{X: body.X + completionTextInset, Y: body.Y + 3, Width: body.Width - completionTextInset - completionTextEnd, Height: body.Height - 6}
	canvas.Clip(text)
	canvas.Label(state.messages.Completion.Loading, text, gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.faint})
	canvas.Unclip()
}

func (state *State) paintCompletionRows(canvas *gfx.Canvas, body gfx.Rect, visible int) {
	list := &state.completion.list
	mainStyle := gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.text}
	descStyle := gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.faint}
	selectedText := gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.onAccent}
	selectedDesc := gfx.TextStyle{Size: metrics.FontSizeS, Color: state.pal.onAccent}
	icons := state.completionShowsIcons()
	textStart := completionTextInset
	if icons {
		textStart = completionTextStart
	}
	for row := 0; row < visible; row++ {
		index := list.top + row
		if index >= len(list.candidates) {
			return
		}
		candidate := &list.candidates[index]
		bounds := gfx.Rect{X: body.X, Y: body.Y + float32(row)*completionRowHeight, Width: body.Width, Height: completionRowHeight}
		id := hitCompletionList.At(index)
		selected := index == list.selected
		hovered := canvas.Hovered() == id
		switch {
		case selected:
			canvas.Rounded(bounds, radius.item, state.pal.accent)
		case hovered:
			// The menus' own hover fill, so a row is as clickable as they are.
			canvas.Rounded(bounds, radius.item, state.pal.surfaceHot)
		}
		canvas.HitWithOptions(id, bounds, candidate.Text, gfx.HitOptions{
			// The grid keeps the keyboard and the IME; the list is operated by
			// the completion keys and by the pointer, so a press must not move
			// focus to this row and take the terminal's keys with it.
			Role: gfx.RoleOption, SkipTab: true, SkipFocus: true, Selected: selected, Cursor: gfx.PointerCursorHand,
		})
		if candidate.Kind == completion.KindPath {
			icon := lucideIcons[iconFile]
			if candidate.Dir {
				icon = lucideIcons[iconFolderOpen]
			}
			iconColor := state.pal.faint
			switch {
			case selected:
				iconColor = state.pal.onAccent
			case hovered:
				iconColor = state.pal.text
			}
			iconRect := gfx.Rect{
				X: bounds.X + completionIconInset, Y: bounds.Y + (bounds.Height-completionIconSize)/2,
				Width: completionIconSize, Height: completionIconSize,
			}
			canvas.DrawIcon(icon, iconRect, iconColor)
		}
		text := gfx.Rect{X: bounds.X + textStart, Y: bounds.Y + 3, Width: bounds.Width - textStart - completionTextEnd, Height: bounds.Height - 6}
		textStyle, detailStyle := mainStyle, descStyle
		if selected {
			textStyle, detailStyle = selectedText, selectedDesc
		}
		if candidate.Desc != "" {
			detailWidth := canvas.TextWidth(candidate.Desc, metrics.FontSizeS)
			if detailWidth < bounds.Width*0.55 {
				text.Width = max(text.Width-detailWidth-10, 10)
				detail := gfx.Rect{X: bounds.X + bounds.Width - detailWidth - completionTextEnd, Y: bounds.Y + 3, Width: detailWidth, Height: bounds.Height - 6}
				canvas.Label(candidate.Desc, detail, detailStyle)
			}
		}
		canvas.Clip(text)
		canvas.Label(candidate.Text, text, textStyle)
		canvas.Unclip()
	}
}

// completionWidth measures the widest row, within the list's bounds.
func (state *State) completionWidth(canvas *gfx.Canvas) float32 {
	width := completionMinWidth
	textStart := completionTextInset
	if state.completionShowsIcons() {
		textStart = completionTextStart
	}
	if len(state.completion.list.candidates) == 0 {
		return min(max(width, canvas.TextWidth(state.messages.Completion.Loading, metrics.FontSizeS)+textStart+completionTextEnd), completionMaxWidth)
	}
	for _, candidate := range state.completion.list.candidates {
		measured := canvas.TextWidth(candidate.Text, metrics.FontSizeS) + textStart + completionTextEnd
		if candidate.Desc != "" {
			measured += 10 + canvas.TextWidth(candidate.Desc, metrics.FontSizeS)
		}
		width = max(width, measured)
	}
	return min(width, completionMaxWidth)
}

// completionShowsIcons reserves the glyph column only when the list has a path
// row. A command or option gets no invented glyph, and a list without paths
// keeps its text at the nearer edge.
func (state *State) completionShowsIcons() bool {
	for _, candidate := range state.completion.list.candidates {
		if candidate.Kind == completion.KindPath {
			return true
		}
	}
	return false
}

// completionEndsInput reports whether input ends the line an automatic list
// follows: Enter, Tab, an interrupt, or an escape sequence such as an arrow.
// Ordinary editing keys keep it open; the echo rebuilds it.
func completionEndsInput(input []byte) bool {
	for _, b := range input {
		switch b {
		case '\r', '\n', '\t', 0x03, 0x04, 0x1b:
			return true
		}
	}
	return false
}

// filterPrefixEntries removes entries whose command is a prefix of another
// entry's command.
func filterPrefixEntries(entries []cmdhistory.Entry) []cmdhistory.Entry {
	filtered := make([]cmdhistory.Entry, 0, len(entries))
	for i, e := range entries {
		isPrefix := false
		for j, other := range entries {
			if i != j && strings.HasPrefix(other.Command, e.Command) && len(other.Command) > len(e.Command) {
				isPrefix = true
				break
			}
		}
		if !isPrefix {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// prefixWordEnd cuts a shared prefix back to its last whole word, the way zsh
// and fish complete: the fragment before a half-typed word is what the user can
// accept without leaving something unrunnable on the line.
func prefixWordEnd(prefix string) string {
	end := strings.LastIndexAny(prefix, " \t")
	if end < 0 {
		return prefix
	}
	return prefix[:end+1]
}

// entryCommands extracts the command strings from entries.
func entryCommands(entries []cmdhistory.Entry) []string {
	commands := make([]string, len(entries))
	for i, e := range entries {
		commands[i] = e.Command
	}
	return commands
}

// humanRelativeTime says how long ago a history match was used, in the words
// the interface already uses for time. The month bucket runs to a year so no
// age renders as a zero year: 360 days is twelve months, not "0y ago".
func humanRelativeTime(now, t time.Time, m i18n.CompletionMessages) string {
	if t.IsZero() {
		return m.TimeJustNow
	}
	d := now.Sub(t)
	if d < 0 {
		return m.TimeJustNow
	}
	switch {
	case d < time.Minute:
		return m.TimeJustNow
	case d < time.Hour:
		return fmt.Sprintf(m.TimeMinutesFmt, int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf(m.TimeHoursFmt, int(d/time.Hour))
	case d < 30*24*time.Hour:
		return fmt.Sprintf(m.TimeDaysFmt, int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf(m.TimeMonthsFmt, int(d/(30*24*time.Hour)))
	default:
		return fmt.Sprintf(m.TimeYearsFmt, int(d/(365*24*time.Hour)))
	}
}

// staticGhost is the inline suggestion the catalog adds when history has none:
// only when exactly one candidate exists, so the ghost never guesses.
func (state *State) staticGhost(session *Session, line string) (string, bool) {
	if strings.TrimSpace(line) == "" || cmdhistory.Secret(line) || !state.completionEligible(session) {
		return "", false
	}
	catalog := state.completionCatalog()
	if catalog == nil {
		return "", false
	}
	result := completion.Complete(catalog, line)
	if result.Suppressed || len(result.Candidates) != 1 {
		return "", false
	}
	return result.Candidates[0].Insert, result.Candidates[0].Insert != ""
}
