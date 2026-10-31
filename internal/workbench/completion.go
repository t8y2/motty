package workbench

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/cmdhistory"
	"motty/internal/completion"
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
)

// completionMode is how much of command completion is on. It is stored in two
// existing preferences, so a configuration written before the modes existed
// reads back unchanged: suggestOff is Off, and inlineOff, with completion on,
// is List.
type completionMode uint8

const (
	completionOff  completionMode = iota // no list, no inline suggestion
	completionList                       // only the list the user asks for
	completionBoth                       // the list and the gray inline suggestion
)

func (state *State) completionMode() completionMode {
	switch {
	case state.history.suggestOff:
		return completionOff
	case state.history.inlineOff:
		return completionList
	default:
		return completionBoth
	}
}

// setCompletionMode reports whether the mode changed. Turning completion on
// starts the work it needs here, in answer to the user, because nothing is
// loaded while it is off.
func (state *State) setCompletionMode(mode completionMode) bool {
	if state.completionMode() == mode {
		return false
	}
	state.history.suggestOff = mode == completionOff
	state.history.inlineOff = mode == completionList
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

func (state *State) settingsCompletionOptions() [3]components.SegmentOption {
	m := state.messages.Completion
	return [...]components.SegmentOption{
		{ID: hitSettingsCompletionOff, Label: m.ModeOff},
		{ID: hitSettingsCompletionList, Label: m.ModeList},
		{ID: hitSettingsCompletionBoth, Label: m.ModeBoth},
	}
}

func (state *State) selectedCompletionOption() ui.ID {
	switch state.completionMode() {
	case completionOff:
		return hitSettingsCompletionOff
	case completionList:
		return hitSettingsCompletionList
	default:
		return hitSettingsCompletionBoth
	}
}

// completionScope names the popover's focus scope; it is separate from the
// terminal's so the grid keeps the keyboard and the IME.
var completionScope = tree.Sub(completionHits, tree.Name("completion-scope"))

const (
	// completionRows is how many candidates the popover lists at once; the rest
	// scroll with the arrow keys.
	completionRows = 10
	// completionRowHeight is one candidate row.
	completionRowHeight = float32(24)
	completionMinWidth  = float32(240)
	completionMaxWidth  = float32(560)
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
	checked       time.Time
	updateReady   chan completionUpdateResult
	cancelUpdate  context.CancelFunc

	// History reads fill history.ghost, the single cache used by both modes.
	// The catalog never waits for these background requests.
	historyPending map[string]bool
	historyReady   chan completionHistoryLoad
	// generation invalidates loads started before the history changed under
	// them: a cleared or newly disabled history must not be refilled by an
	// answer to an old question.
	generation uint64

	list completionPopup
}

type completionCatalogLoad struct {
	err  error
	note error
}

type completionUpdateResult struct {
	err error
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
	suggestionSnapshot

	candidates []completion.Candidate
	selected   int
	top        int
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
	// With completion off nothing needs the catalog, so it is not decoded: it
	// costs about ten megabytes of heap per process.
	if state.history.suggestOff && !state.completion.updatePending {
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
	catalog, generation := c.store.Snapshot()
	if catalog != nil && generation != c.catalogGeneration {
		c.catalog, c.catalogGeneration = catalog, generation
		state.closeCompletionList()
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
		oldGeneration := c.catalogGeneration
		state.adoptCompletionCatalog()
		if result.err == nil && c.catalog != nil {
			if c.catalogGeneration != oldGeneration {
				c.catalogNote = nil
				state.status = fmt.Sprintf(state.messages.Completion.UpdatedFmt, c.catalog.Release)
			} else {
				state.status = state.messages.Completion.UpToDate
			}
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
	if state.history.suggestOff || !state.history.on || state.historyClearing() || !state.historyAvailable(session) || session.host.NoHistory {
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
		_, _, err := store.Update(ctx, completion.UpdateClient())
		ready <- completionUpdateResult{err: err}
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
		return fmt.Sprintf(m.CheckFailedFmt, c.updateErr), c.updateErr
	}
	if c.catalog == nil {
		if c.catalogErr != nil {
			return fmt.Sprintf(m.CheckFailedFmt, c.catalogErr), c.catalogErr
		}
		if state.history.suggestOff && !c.updatePending {
			return m.CatalogOff, nil
		}
		return m.CatalogLoading, nil
	}
	status := fmt.Sprintf(m.CatalogDescriptionFmt, c.catalog.Release, c.catalog.RootCount(), c.catalog.FlagCount())
	if c.catalogNote != nil {
		status = fmt.Sprintf(m.CacheFailedFmt, status, c.catalogNote)
	}
	if !c.checked.IsZero() {
		status = fmt.Sprintf(m.CheckedFmt, status, c.checked.Format("2006-01-02 15:04"))
	}
	return status, nil
}

// completionEligible reports whether this session may show completions now.
// The catalog is public data, but a list still needs a live prompt whose input
// the client can confirm: no dialog, no password prompt, no overlay a program
// draws itself.
func (state *State) completionEligible(session *Session) bool {
	if session == nil || session.term == nil || session.host.NoHistory {
		return false
	}
	if session.term.suggestionPending != nil || session.term.preedit.Composing {
		return false
	}
	return state.inputGuardAvailable(session)
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
	t.completionInput = func() {
		if state.completion.list.active && state.completion.list.sessionID == sessionID {
			state.closeCompletionList()
		}
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
// shortcut, the command palette entry, and the mouse handler.
func (state *State) openCompletions() {
	if state.history.suggestOff {
		return // off means neither the list nor the ghost text
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !state.completionEligible(session) {
		return
	}
	state.openCompletionList(session)
}

// openCompletionList builds the initial candidates. An empty prompt or an
// unsupported line leaves the list closed rather than showing a stray frame.
func (state *State) openCompletionList(session *Session) {
	t := session.term
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok || strings.TrimSpace(line) == "" {
		return
	}
	candidates := state.completionCandidates(session, line)
	if len(candidates) == 0 {
		return
	}
	state.completion.list = completionPopup{
		active:             true,
		suggestionSnapshot: state.captureSuggestion(session, line),
		candidates:         candidates,
	}
}

// closeCompletionList forgets the list; no data is touched.
func (state *State) closeCompletionList() {
	state.completion.list = completionPopup{}
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
	candidates := state.completionCandidates(session, line)
	if len(candidates) == 0 {
		state.closeCompletionList()
		return
	}
	list.suggestionSnapshot = state.captureSuggestion(session, line)
	list.candidates = candidates
	if list.selected >= len(candidates) {
		list.selected = len(candidates) - 1
	}
	list.clamp()
}

// completionRefresh follows output that may have changed the prompt. It is
// cheap: the recorder only reports a line when the cursor is at its end.
func (state *State) completionRefresh(session *Session) {
	t := session.term
	if t == nil || !state.completion.list.active || state.completion.list.sessionID != session.ID {
		return
	}
	if state.modal != modalNone || !state.completionEligible(session) {
		state.closeCompletionList()
		return
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok || line == state.completion.list.line {
		return
	}
	state.completionRebuild(session)
}

// completionCandidates merges the host's history, newest first, with the
// catalog's stable order. Duplicates are dropped by what they would insert.
func (state *State) completionCandidates(session *Session, line string) []completion.Candidate {
	if cmdhistory.Secret(line) {
		return nil
	}
	candidates := make([]completion.Candidate, 0, 16)
	seen := make(map[string]struct{}, 16)
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
	if state.history.on && state.historyAvailable(session) && !session.host.NoHistory {
		if rest, ok := cmdhistory.Suggest(state.completionHistory(session), line); ok {
			add(completion.Candidate{Text: line + rest, Insert: rest, Kind: completion.KindCommand})
		}
	}
	if catalog := state.completionCatalog(); catalog != nil {
		if result := completion.Complete(catalog, line); !result.Suppressed {
			for _, candidate := range result.Candidates {
				add(candidate)
			}
		}
	}
	return candidates
}

// completionKey handles keys while the list is open. Printable keys stay with
// the terminal; the list closes on the next typed byte.
func (state *State) completionKey(event ui.KeyEvent) (ui.EventResult, bool) {
	list := &state.completion.list
	if !list.active {
		return ui.EventResult{}, false
	}
	if state.modal != modalNone {
		state.closeCompletionList()
		return ui.EventResult{}, false
	}
	session := state.completionSession()
	if session == nil || session.term.preedit.Composing {
		state.closeCompletionList()
		return ui.EventResult{}, false
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	switch event.Key {
	case ui.KeyEscape:
		state.closeCompletionList()
		return handled, true
	case ui.KeyEnter:
		if event.Repeat {
			return handled, true
		}
		if event.Modifiers != 0 {
			state.closeCompletionList()
			return ui.EventResult{}, false
		}
		state.acceptCompletion(session)
		return handled, true
	case ui.KeyTab:
		if event.Modifiers != 0 {
			return ui.EventResult{}, false
		}
		state.closeCompletionList()
		return ui.EventResult{}, false
	case ui.KeyUp:
		if event.Modifiers != 0 {
			return ui.EventResult{}, false
		}
		state.completionMove(-1)
		return handled, true
	case ui.KeyDown:
		if event.Modifiers != 0 {
			return ui.EventResult{}, false
		}
		state.completionMove(1)
		return handled, true
	case ui.KeyPageUp:
		state.completionMove(-completionRows)
		return handled, true
	case ui.KeyPageDown:
		state.completionMove(completionRows)
		return handled, true
	case ui.KeyHome:
		list.selected, list.top = 0, 0
		return handled, true
	case ui.KeyEnd:
		list.selected = len(list.candidates) - 1
		list.clamp()
		return handled, true
	}
	return ui.EventResult{}, false
}

func (list *completionPopup) clamp() {
	if list.selected < 0 {
		list.selected = 0
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
	list.selected += delta
	list.clamp()
}

// acceptCompletion inserts the selected suffix, and only that. Before typing it
// drains queued output and rechecks the session, terminal, prompt version and
// line the list was built from; a mismatch drops the acceptance silently.
func (state *State) acceptCompletion(session *Session) {
	list := state.completion.list
	state.closeCompletionList()
	if !list.active || list.selected < 0 || list.selected >= len(list.candidates) {
		return
	}
	session = state.suggestionOwner(list.suggestionSnapshot)
	if session == nil || !state.completionEligible(session) {
		return
	}
	state.sendSuggestion(session, list.candidates[list.selected].Insert)
}

// clickCompletion handles the list's controls.
func (state *State) clickCompletion(id ui.ID) bool {
	if id == hitCompletionOpen {
		state.openCompletions()
		return true
	}
	if id == hitCompletionDismiss {
		state.closeCompletionList()
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
func (state *State) paintCompletionList(canvas *ui.Canvas, session *Session, geometry terminalGeometry) {
	list := &state.completion.list
	if !list.active || list.sessionID != session.ID || state.modal != modalNone {
		return
	}
	if !state.completionEligible(session) || len(list.candidates) == 0 {
		return
	}
	cursor := session.term.presentedCursor()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	x := geometry.left + float32(cursor.X)*cellWidth
	y := geometry.top + float32(cursor.Y)*cellHeight
	area := geometry.area
	x = min(max(x, area.X+2), area.X+max(area.Width-2, 2))
	y = min(max(y, area.Y+2), area.Y+max(area.Height-2, 2))
	anchor := ui.Rect{X: x, Y: y, Width: max(cellWidth, 2), Height: max(cellHeight, 2)}

	visible := min(len(list.candidates), completionRows)
	width := state.completionWidth(canvas)
	bodyHeight := float32(visible)*completionRowHeight + 4
	popover := components.BeginPopover(canvas, state.Theme(), area, anchor, components.PopoverOptions{
		Layer:       dialogLayer,
		Scope:       completionScope.ID(tree.Name("popover")),
		DismissID:   hitCompletionDismiss,
		Side:        components.PopoverBottom,
		Align:       components.PopoverAlignStart,
		SideOffset:  2,
		ContentSize: ui.Size{Width: width},
		BodyHeight:  bodyHeight,
	})
	layout := popover.Begin(state.messages.Completion.Title, "")
	state.paintCompletionRows(canvas, layout.Body, visible)
	popover.End()
}

func (state *State) paintCompletionRows(canvas *ui.Canvas, body ui.Rect, visible int) {
	list := &state.completion.list
	mainStyle := ui.TextStyle{Size: metrics.FontSizeS, Color: state.pal.text}
	descStyle := ui.TextStyle{Size: metrics.FontSizeS, Color: state.pal.faint}
	selectedText := ui.TextStyle{Size: metrics.FontSizeS, Color: state.pal.onAccent}
	selectedDesc := ui.TextStyle{Size: metrics.FontSizeS, Color: state.pal.onAccent}
	for row := 0; row < visible; row++ {
		index := list.top + row
		if index >= len(list.candidates) {
			return
		}
		candidate := &list.candidates[index]
		bounds := ui.Rect{X: body.X, Y: body.Y + float32(row)*completionRowHeight, Width: body.Width, Height: completionRowHeight}
		selected := index == list.selected
		if selected {
			canvas.Rounded(bounds, radius.item, state.pal.accent)
		}
		canvas.HitWithOptions(hitCompletionList.At(index), bounds, candidate.Text, ui.HitOptions{Role: ui.RoleOption, SkipTab: true, Selected: selected})
		text := ui.Rect{X: bounds.X + 8, Y: bounds.Y + 3, Width: bounds.Width - 16, Height: bounds.Height - 6}
		textStyle, detailStyle := mainStyle, descStyle
		if selected {
			textStyle, detailStyle = selectedText, selectedDesc
		}
		if candidate.Desc != "" {
			detailWidth := canvas.TextWidth(candidate.Desc, metrics.FontSizeS)
			if detailWidth < bounds.Width*0.55 {
				text.Width = max(bounds.Width-16-detailWidth-10, 10)
				detail := ui.Rect{X: bounds.X + bounds.Width - detailWidth - 8, Y: bounds.Y + 3, Width: detailWidth, Height: bounds.Height - 6}
				canvas.Label(candidate.Desc, detail, detailStyle)
			}
		}
		canvas.Clip(text)
		canvas.Label(candidate.Text, text, textStyle)
		canvas.Unclip()
	}
}

// completionWidth measures the widest row, within the list's bounds.
func (state *State) completionWidth(canvas *ui.Canvas) float32 {
	width := completionMinWidth
	for _, candidate := range state.completion.list.candidates {
		measured := canvas.TextWidth(candidate.Text, metrics.FontSizeS) + 16
		if candidate.Desc != "" {
			measured += 10 + canvas.TextWidth(candidate.Desc, metrics.FontSizeS)
		}
		width = max(width, measured)
	}
	return min(width, completionMaxWidth)
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
