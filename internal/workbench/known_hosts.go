package workbench

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"motty/internal/i18n"
	"motty/internal/knownhosts"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

const (
	defaultKnownHostsPageSize = 15
	knownHostsColumns         = 7
	// knownHostsSidePad is the page's padding beside the table, on each side.
	knownHostsSidePad = float32(18)
)

var (
	knownHostsPageSizes      = [...]int{15, 30, 50, 100}
	knownHostsPageSizeLabels = [...]string{"15", "30", "50", "100"}
)

type knownHostsAction uint8

const (
	knownHostsActionNone knownHostsAction = iota
	knownHostsActionDelete
	knownHostsActionRetrust
)

type knownHostRecord struct {
	entry       knownhosts.Entry
	selected    bool
	cells       [7]string
	searchText  string
	selectLabel string
	// rowLabel names the row in assistive technology by all its non-empty cells.
	rowLabel string
}

// knownHostRowLabel joins the cells the row shows into one spoken name. It runs
// when the file is read, not in the paint.
func knownHostRowLabel(cells [7]string) string {
	var b strings.Builder
	for _, cell := range cells {
		if cell == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(cell)
	}
	return b.String()
}

type knownHostsView struct {
	file    *knownhosts.File
	rows    []knownHostRecord
	visible []int
	search  text.Editor
	// searchField is the page's text field over search, bound when first declared.
	searchField     ui.Field
	searchBound     bool
	visibleQuery    string
	visibleLanguage i18n.Language
	synced          bool
	page            int
	pageSize        int
	// pageSizeChoice is what the page-size switcher edits; pageSize changes only
	// through setKnownHostsPageSize.
	pageSizeChoice       int
	selected             int
	countText            string
	selectionText        string
	pageText             string
	pageNumberText       string
	status               string
	errorText            string
	action               knownHostsAction
	dialogDescription    string
	dialogError          string
	dialogErrorSelection ui.TextSelectionState // what the user selected of dialogError, to copy it
	paginationItems      [3]ui.PaginationItem
	// list and rowDecl are the page's table: its rows and the builder that
	// declares one. columns and headings are set on each paint so the list's
	// cells line up with the header's.
	list     ui.ListState
	rowDecl  func(ui.ListRow)
	columns  [knownHostsColumns]components.TableColumn
	headings [knownHostsColumns]string
}

func (state *State) initKnownHosts() {
	view := &state.knownHosts
	view.file = knownhosts.New(state.options.KnownHostsPath)
	view.pageSize = defaultKnownHostsPageSize
	view.rowDecl = state.declareKnownHostRow
	view.list.Key = func(row int) int {
		start, _ := state.knownHostsPageRange()
		if at := start + row; at >= 0 && at < len(view.visible) {
			return view.visible[at]
		}
		return row
	}
	view.list.Label = func(row int) string {
		start, _ := state.knownHostsPageRange()
		if at := start + row; at >= 0 && at < len(view.visible) {
			return view.rows[view.visible[at]].selectLabel
		}
		return ""
	}
}

func (state *State) openKnownHosts() {
	state.setActiveSection(sectionKnownHosts)
	state.refreshKnownHosts(true)
}

func (state *State) refreshKnownHosts(report bool) {
	view := &state.knownHosts
	entries, err := view.file.List()
	if errors.Is(err, fs.ErrNotExist) {
		entries, err = nil, nil
	}
	if err != nil {
		view.rows = view.rows[:0]
		view.visible = view.visible[:0]
		view.selected = 0
		view.synced = false
		view.errorText = fmt.Sprintf(state.messages.KnownHostsLoadFailedFmt, err)
		view.status = view.errorText
		return
	}

	if cap(view.rows) < len(entries) {
		view.rows = make([]knownHostRecord, len(entries))
	} else {
		view.rows = view.rows[:len(entries)]
	}
	for index, entry := range entries {
		hosts := strings.Join(entry.HostPatterns, ",")
		flags := entry.Marker
		if entry.Hashed {
			if flags != "" {
				flags += " · "
			}
			flags += "|1|"
		}
		line := strconv.Itoa(entry.Line)
		cells := [7]string{"", line, hosts, flags, entry.KeyType, entry.Fingerprint, entry.Comment}
		view.rows[index] = knownHostRecord{
			entry: entry,
			cells: cells,
			searchText: strings.ToLower(strings.Join([]string{
				hosts, entry.KeyType, entry.Fingerprint, entry.Comment,
			}, " ")),
			selectLabel: hosts + " · " + entry.Fingerprint,
			rowLabel:    knownHostRowLabel(cells),
		}
	}
	view.selected = 0
	view.page = 0
	view.list.Select(-1)
	view.list.ScrollTo(0, ui.ListStart)
	view.synced = false
	view.errorText = ""
	state.syncKnownHostsVisible()
	if report {
		view.status = ""
	}
}

func (state *State) syncKnownHostsVisible() {
	view := &state.knownHosts
	query := view.search.String()
	if view.synced && query == view.visibleQuery && view.visibleLanguage == state.language {
		return
	}
	languageChanged := view.synced && view.visibleLanguage != state.language
	queryChanged := !view.synced || query != view.visibleQuery
	view.visibleQuery = query
	view.visibleLanguage = state.language
	view.synced = true
	if queryChanged {
		view.page = 0
		view.list.Select(-1)
		view.list.ScrollTo(0, ui.ListStart)
		needle := strings.ToLower(strings.TrimSpace(query))
		view.visible = view.visible[:0]
		if view.visible == nil {
			view.visible = make([]int, 0, len(view.rows))
		}
		for index := range view.rows {
			if needle == "" || strings.Contains(view.rows[index].searchText, needle) {
				view.visible = append(view.visible, index)
			}
		}
	}
	state.clampKnownHostsPage()
	state.refreshKnownHostsSummaries()
	if languageChanged && view.errorText == "" {
		view.status = ""
	}
}

func (state *State) knownHostsPageSize() int {
	size := state.knownHosts.pageSize
	if size <= 0 {
		return defaultKnownHostsPageSize
	}
	return size
}

func (state *State) setKnownHostsPageSize(size int) {
	valid := false
	for _, option := range knownHostsPageSizes {
		if option == size {
			valid = true
			break
		}
	}
	if !valid || state.knownHosts.pageSize == size {
		return
	}
	state.knownHosts.pageSize = size
	state.knownHosts.page = 0
	state.knownHosts.list.Select(-1)
	state.knownHosts.list.ScrollTo(0, ui.ListStart)
	state.clampKnownHostsPage()
	state.refreshKnownHostsSummaries()
}

// knownHostsPageSizeLabel names a page size without allocating: the switcher asks
// for the labels on every paint.
func knownHostsPageSizeLabel(size int) string {
	for i, option := range knownHostsPageSizes {
		if option == size {
			return knownHostsPageSizeLabels[i]
		}
	}
	return strconv.Itoa(size)
}

func (state *State) clampKnownHostsPage() {
	view := &state.knownHosts
	pageSize := state.knownHostsPageSize()
	pages := max(1, (len(view.visible)+pageSize-1)/pageSize)
	view.page = min(max(0, view.page), pages-1)
}

func (state *State) refreshKnownHostsSummaries() {
	view := &state.knownHosts
	if strings.TrimSpace(view.visibleQuery) == "" {
		view.countText = fmt.Sprintf(state.messages.KnownHostsCountFmt, len(view.rows))
	} else {
		view.countText = fmt.Sprintf(state.messages.KnownHostsFilteredCountFmt, len(view.visible), len(view.rows))
	}
	view.selectionText = fmt.Sprintf(state.messages.KnownHostsSelectedFmt, view.selected)
	pageSize := state.knownHostsPageSize()
	pages := max(1, (len(view.visible)+pageSize-1)/pageSize)
	view.pageText = fmt.Sprintf(state.messages.KnownHostsPageFmt, view.page+1, pages)
	view.pageNumberText = strconv.Itoa(view.page + 1)
}

func (state *State) knownHostsPageRange() (int, int) {
	view := &state.knownHosts
	pageSize := state.knownHostsPageSize()
	start := min(view.page*pageSize, len(view.visible))
	return start, min(start+pageSize, len(view.visible))
}

func (state *State) clickKnownHosts(id gfx.ID) bool {
	view := &state.knownHosts
	if state.activeSection != sectionKnownHosts {
		return false
	}
	switch id {
	case hitKnownHostsSearch:
		return true
	case hitKnownHostsRefresh:
		state.refreshKnownHosts(true)
		return true
	case hitKnownHostsDelete:
		state.openKnownHostsAction(knownHostsActionDelete)
		return true
	case hitKnownHostsRetrust:
		state.openKnownHostsAction(knownHostsActionRetrust)
		return true
	case hitKnownHostsPrevious:
		if view.page > 0 {
			view.page--
			view.list.Select(-1)
			view.list.ScrollTo(0, ui.ListStart)
			state.refreshKnownHostsSummaries()
		}
		return true
	case hitKnownHostsNext:
		pageSize := state.knownHostsPageSize()
		pages := max(1, (len(view.visible)+pageSize-1)/pageSize)
		if view.page+1 < pages {
			view.page++
			view.list.Select(-1)
			view.list.ScrollTo(0, ui.ListStart)
			state.refreshKnownHostsSummaries()
		}
		return true
	}
	if index, ok := state.knownHostIndexForHit(id); ok {
		view.rows[index].selected = !view.rows[index].selected
		if view.rows[index].selected {
			view.selected++
		} else {
			view.selected--
		}
		state.refreshKnownHostsSummaries()
		return true
	}
	return false
}

func (state *State) knownHostIndexForHit(id gfx.ID) (int, bool) {
	var index int
	switch {
	case id.Scope() == hitKnownHostRowList:
		index = item(id)
	case id.Scope() == hitKnownHostCheckList:
		index = item(id)
	default:
		return 0, false
	}
	return int(index), true
}

func (state *State) openKnownHostsAction(action knownHostsAction) {
	view := &state.knownHosts
	if view.selected == 0 {
		return
	}
	view.action = action
	view.dialogError = ""
	if action == knownHostsActionRetrust {
		view.dialogDescription = fmt.Sprintf(state.messages.KnownHostsRetrustDescriptionFmt, view.selected)
	} else {
		view.dialogDescription = fmt.Sprintf(state.messages.KnownHostsDeleteDescriptionFmt, view.selected)
	}
}

func (state *State) clickKnownHostsDialog(id gfx.ID) {
	view := &state.knownHosts
	switch id {
	case hitKnownHostsDialogDismiss, hitKnownHostsDialogClose, hitKnownHostsDialogCancel:
		view.action = knownHostsActionNone
		view.dialogError = ""
	case hitKnownHostsDialogConfirm:
		state.confirmKnownHostsAction()
	}
}

func (state *State) confirmKnownHostsAction() {
	view := &state.knownHosts
	if view.action == knownHostsActionNone || view.selected == 0 {
		return
	}
	ids := make([]knownhosts.ID, 0, view.selected)
	for index := range view.rows {
		if view.rows[index].selected {
			ids = append(ids, view.rows[index].entry.ID)
		}
	}
	action, count := view.action, len(ids)
	if err := view.file.DeleteIDs(ids...); err != nil {
		view.dialogError = fmt.Sprintf(state.messages.KnownHostsDeleteFailedFmt, err)
		return
	}
	view.action = knownHostsActionNone
	view.dialogError = ""
	state.refreshKnownHosts(false)
	if action == knownHostsActionRetrust {
		view.status = fmt.Sprintf(state.messages.KnownHostsRetrustedFmt, count)
	} else {
		view.status = fmt.Sprintf(state.messages.KnownHostsDeletedFmt, count)
	}
}

func (state *State) knownHostsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	view := &state.knownHosts
	if view.action != knownHostsActionNone {
		if event.Key == gfx.KeyEnter && event.Target == hitKnownHostsDialogConfirm && !event.Repeat {
			state.confirmKnownHostsAction()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	if state.activeSection != sectionKnownHosts || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape && event.Target == hitKnownHostsSearch && view.search.RuneCount() > 0 {
		view.search = text.Editor{}
		view.synced = false
		state.syncKnownHostsVisible()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if _, ok := state.knownHostIndexForHit(event.Target); ok && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		state.clickKnownHosts(event.Target)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key == gfx.KeyDelete && view.selected > 0 {
		state.openKnownHostsAction(knownHostsActionDelete)
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}
