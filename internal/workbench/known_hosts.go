package workbench

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/knownhosts"
)

const knownHostsPageSize = 15

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
}

type knownHostsView struct {
	file              *knownhosts.File
	rows              []knownHostRecord
	visible           []int
	search            text.Editor
	visibleQuery      string
	visibleLanguage   i18n.Language
	synced            bool
	page              int
	selected          int
	countText         string
	selectionText     string
	pageText          string
	status            string
	errorText         string
	action            knownHostsAction
	dialogDescription string
	dialogError       string
}

func (state *State) initKnownHosts() {
	state.knownHosts.file = knownhosts.New(state.options.KnownHostsPath)
}

func (state *State) openKnownHosts() {
	state.activeSection = sectionKnownHosts
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
		view.rows[index] = knownHostRecord{
			entry: entry,
			cells: [7]string{
				"", line, hosts, flags, entry.KeyType, entry.Fingerprint, entry.Comment,
			},
			searchText: strings.ToLower(strings.Join([]string{
				hosts, entry.KeyType, entry.Fingerprint, entry.Comment,
			}, " ")),
			selectLabel: hosts + " · " + entry.Fingerprint,
		}
	}
	view.selected = 0
	view.page = 0
	view.synced = false
	view.errorText = ""
	state.syncKnownHostsVisible()
	if report {
		view.status = fmt.Sprintf(state.messages.KnownHostsLoadedFmt, len(view.rows))
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
		view.status = fmt.Sprintf(state.messages.KnownHostsLoadedFmt, len(view.rows))
	}
}

func (state *State) clampKnownHostsPage() {
	view := &state.knownHosts
	pages := max(1, (len(view.visible)+knownHostsPageSize-1)/knownHostsPageSize)
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
	pages := max(1, (len(view.visible)+knownHostsPageSize-1)/knownHostsPageSize)
	view.pageText = fmt.Sprintf(state.messages.KnownHostsPageFmt, view.page+1, pages)
}

func (state *State) knownHostsPageRange() (int, int) {
	view := &state.knownHosts
	start := min(view.page*knownHostsPageSize, len(view.visible))
	return start, min(start+knownHostsPageSize, len(view.visible))
}

func (state *State) clickKnownHosts(id ui.ID) bool {
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
			state.refreshKnownHostsSummaries()
		}
		return true
	case hitKnownHostsNext:
		pages := max(1, (len(view.visible)+knownHostsPageSize-1)/knownHostsPageSize)
		if view.page+1 < pages {
			view.page++
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
		view.status = view.selectionText
		return true
	}
	return false
}

func (state *State) knownHostIndexForHit(id ui.ID) (int, bool) {
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

func (state *State) clickKnownHostsDialog(id ui.ID) {
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

func (state *State) knownHostsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	view := &state.knownHosts
	if view.action != knownHostsActionNone {
		if event.Key == ui.KeyEnter && event.Target == hitKnownHostsDialogConfirm && !event.Repeat {
			state.confirmKnownHostsAction()
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		return ui.EventResult{}, false
	}
	if state.activeSection != sectionKnownHosts || event.Repeat {
		return ui.EventResult{}, false
	}
	if event.Key == ui.KeyEscape && event.Target == hitKnownHostsSearch && view.search.RuneCount() > 0 {
		view.search = text.Editor{}
		view.synced = false
		state.syncKnownHostsVisible()
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if index, ok := state.knownHostIndexForHit(event.Target); ok && (event.Key == ui.KeyEnter || event.Key == ui.KeySpace) {
		state.clickKnownHosts(hitKnownHostRowList.At(index))
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key == ui.KeyDelete && view.selected > 0 {
		state.openKnownHostsAction(knownHostsActionDelete)
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}
