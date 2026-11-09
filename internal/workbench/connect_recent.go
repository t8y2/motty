package workbench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var (
	hitConnectSortDefault    = tree.ID(commandPaletteHits, "connect-sort-default")
	hitConnectSortWorkspaces = tree.ID(commandPaletteHits, "connect-sort-workspaces")
	hitConnectSortRecent     = tree.ID(commandPaletteHits, "connect-sort-recent")
)

// rememberRecentConnection consumes the user dial once, never a status refresh
// or a retry. Local terminals call this only after their shell has started.
func (state *State) rememberRecentConnection(session *Session) {
	if !session.recordRecent {
		return
	}
	session.recordRecent = false
	key := session.host.CatalogID
	if session.isLocal() {
		key = "local-terminal"
	}
	if key == "" {
		return
	}
	if state.recentConnections == nil {
		state.recentConnections = make(map[string]time.Time)
	}
	state.recentConnections[key] = state.wallNow().UTC()
	state.save()
}

// connectSortOptions is the default/recent switch of the new-tab picker. It is
// one list for painting and for the keyboard, so the two cannot drift.
func (state *State) connectSortOptions() [3]components.SegmentOption {
	return [3]components.SegmentOption{
		{ID: hitConnectSortDefault, Label: state.messages.NewTabSortDefault},
		{ID: hitConnectSortWorkspaces, Label: state.messages.WorkspaceLibraryTitle},
		{ID: hitConnectSortRecent, Label: state.messages.NewTabSortRecent},
	}
}

func (state *State) paintConnectSort(canvas *gfx.Canvas, layout components.DialogLayout) {
	options := state.connectSortOptions()
	selected := hitConnectSortDefault
	if state.commandPalette.workspaces {
		selected = hitConnectSortWorkspaces
	} else if state.recentConnectSort {
		selected = hitConnectSortRecent
	}
	width := float32(270)
	rect := gfx.Rect{X: layout.Close.X - width - 8, Y: layout.Close.Y, Width: width, Height: layout.Close.Height}
	components.BeginSegmentedControl(canvas, state.Theme(), rect, options[:], selected).Paint()
}

// rebuildRecentConnectPicker filters the complete catalog before adding capped
// display rows, so headings describe only the matches and can repeat.
func (state *State) rebuildRecentConnectPicker(rawQuery string) {
	palette := &state.commandPalette
	query := strings.ToLower(rawQuery)
	keywords := strings.Fields(query)
	type candidate struct {
		entry commandPaletteEntry
		group string
		at    time.Time
	}
	var candidates []candidate
	state.eachConnectPickerCandidate(func(item connectPickerCandidate) {
		if item.searchOnly && query == "" && !state.commandPalette.localShells {
			return
		}
		search := strings.ToLower(item.label + " " + item.detail)
		if !matchesSearchWords(search, keywords) {
			return
		}
		group := ""
		if item.group != nil {
			group = state.hostGroupLabel(*item.group)
		}
		candidates = append(candidates, candidate{
			entry: commandPaletteEntry{label: item.label, detail: item.detail, search: search, target: item.target, searchOnly: item.searchOnly},
			group: group, at: state.recentConnections[item.key],
		})
	})
	slices.SortStableFunc(candidates, func(a, b candidate) int { return -a.at.Compare(b.at) })
	palette.entries, palette.visible = palette.entries[:0], palette.visible[:0]
	previousGroup := ""
	now := state.wallNow()
	for _, item := range candidates {
		if item.group != previousGroup && item.group != "" {
			palette.addHeader(item.group, "")
		}
		previousGroup = item.group
		entry := item.entry
		entry.recency = state.connectionRecency(item.at, now)
		if !item.at.IsZero() {
			entry.recencyHint = item.at.Local().Format("2006-01-02 15:04:05 MST")
		}
		palette.entries = append(palette.entries, entry)
	}
	for index := range palette.entries {
		palette.visible = append(palette.visible, index)
	}
	palette.rawQuery, palette.query, palette.filtered = rawQuery, query, true
	palette.selected, palette.scroll = max(palette.choiceAt(0, 1), 0), 0
}

func (state *State) connectionRecency(at, now time.Time) string {
	m := state.messages
	if at.IsZero() {
		return m.NewTabNeverConnected
	}
	seconds := max(int64(now.Sub(at)/time.Second), 0)
	if seconds == 0 {
		return m.NewTabJustNow
	}
	units := [...]struct {
		seconds int64
		format  string
	}{
		{365 * 86400, m.NewTabYearsAgo}, {30 * 86400, m.NewTabMonthsAgo}, {7 * 86400, m.NewTabWeeksAgo},
		{86400, m.NewTabDaysAgo}, {3600, m.NewTabHoursAgo}, {60, m.NewTabMinutesAgo}, {1, m.NewTabSecondsAgo},
	}
	for _, unit := range units {
		if seconds >= unit.seconds {
			return fmt.Sprintf(unit.format, seconds/unit.seconds)
		}
	}
	return m.NewTabJustNow
}
