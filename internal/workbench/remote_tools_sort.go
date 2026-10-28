package workbench

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
)

var remoteColumnCounts = [...]int{remoteProcesses: 6, remoteContainers: 5, remoteImages: 4, remoteVolumes: 3, remoteNetworks: 4, remoteCompose: 3}

func remoteTableHeaderID(tab remoteTab, column int) ui.ID {
	if tab == remoteProcesses {
		if column == 2 {
			return hitRemoteSortCPU
		}
		if column == 3 {
			return hitRemoteSortMemory
		}
	}
	return hitRemoteSortColumns.At(int(tab)*6 + column)
}

func (view *remoteToolsView) tableSort() components.TableSortState {
	order := view.ordering[view.tab]
	if order.Direction != components.TableSortNone {
		return order
	}
	if view.tab == remoteProcesses {
		column := 2
		if view.sort == remotetools.SortMemory {
			column = 3
		}
		return components.TableSortState{Column: column, Direction: components.TableSortDescending}
	}
	// Docker keeps its existing server order (for example, running containers
	// first) until a header is activated.
	return components.TableSortState{}
}

func (state *State) clickRemoteTableSort(id ui.ID) bool {
	view := &state.remote
	for column := 0; column < remoteColumnCounts[view.tab]; column++ {
		if id != remoteTableHeaderID(view.tab, column) {
			continue
		}
		if view.loading {
			return true
		}
		view.ordering[view.tab] = view.tableSort().Toggle(column)
		view.page, view.pageText = 0, ""
		// Preserve the existing bounded top-CPU/top-memory queries. Changing
		// direction or any other field orders the complete loaded snapshot
		// locally; it never changes which stable ID an action targets.
		if view.tab == remoteProcesses && (column == 2 || column == 3) {
			key := remotetools.SortCPU
			if column == 3 {
				key = remotetools.SortMemory
			}
			if key != view.sort {
				view.sort = key
				state.refreshRemoteTools()
				return true
			}
		}
		state.sortRemoteRows()
		return true
	}
	return false
}

func (state *State) sortRemoteRows() {
	view := &state.remote
	order := view.tableSort()
	if order.Direction == components.TableSortNone {
		return
	}
	column := order.Column
	if column < 0 || column >= remoteColumnCounts[view.tab] {
		return
	}
	numeric := view.tab == remoteProcesses && (column == 0 || column == 2 || column == 3 || column == 4) || view.tab == remoteImages && (column == 2 || column == 3)
	for i := range view.rows {
		row := &view.rows[i]
		value := strings.TrimSpace(row.cells[column])
		row.orderText = strings.ToLower(value)
		row.orderValid = value != "" && value != "-" && value != "--" && row.orderText != "n/a" && row.orderText != "<none>"
		if numeric {
			switch {
			case view.tab == remoteProcesses && column == 4:
				row.orderNumber, row.orderValid = remoteElapsedSeconds(value)
			case view.tab == remoteImages && column == 2:
				row.orderNumber, row.orderValid = remoteSizeBytes(value)
			case view.tab == remoteImages && column == 3:
				row.orderNumber, row.orderValid = remoteCreatedAge(value)
				row.orderNumber = -row.orderNumber // chronological order: oldest first
			default:
				var err error
				row.orderNumber, err = strconv.ParseFloat(value, 64)
				row.orderValid = err == nil && !math.IsNaN(row.orderNumber) && !math.IsInf(row.orderNumber, 0)
			}
		}
	}
	slices.SortStableFunc(view.rows, func(a, b remoteRow) int {
		// Missing/unsupported values stay last in either direction.
		if a.orderValid != b.orderValid {
			if a.orderValid {
				return -1
			}
			return 1
		}
		result := strings.Compare(a.orderText, b.orderText)
		if numeric && a.orderValid {
			result = cmp.Compare(a.orderNumber, b.orderNumber)
		}
		if order.Direction == components.TableSortDescending {
			result = -result
		}
		if result == 0 {
			return strings.Compare(a.key, b.key)
		}
		return result
	})
}

func remoteElapsedSeconds(value string) (float64, bool) {
	days := float64(0)
	if before, after, ok := strings.Cut(value, "-"); ok {
		var err error
		days, err = strconv.ParseFloat(before, 64)
		if err != nil || days < 0 {
			return 0, false
		}
		value = after
	}
	parts := strings.Split(value, ":")
	if len(parts) < 1 || len(parts) > 3 {
		return 0, false
	}
	seconds := float64(0)
	for _, part := range parts {
		number, err := strconv.ParseFloat(part, 64)
		if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
			return 0, false
		}
		seconds = seconds*60 + number
	}
	seconds += days * 86400
	return seconds, !math.IsNaN(seconds) && !math.IsInf(seconds, 0)
}

func remoteSizeBytes(value string) (float64, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	index := 0
	for index < len(value) && (value[index] >= '0' && value[index] <= '9' || value[index] == '.') {
		index++
	}
	number, err := strconv.ParseFloat(value[:index], 64)
	if err != nil || number < 0 {
		return 0, false
	}
	unit := strings.TrimSpace(value[index:])
	factor := float64(1)
	switch unit {
	case "B", "":
	case "KB":
		factor = 1e3
	case "MB":
		factor = 1e6
	case "GB":
		factor = 1e9
	case "TB":
		factor = 1e12
	case "PB":
		factor = 1e15
	case "KIB":
		factor = 1 << 10
	case "MIB":
		factor = 1 << 20
	case "GIB":
		factor = 1 << 30
	case "TIB":
		factor = 1 << 40
	case "PIB":
		factor = 1 << 50
	default:
		return 0, false
	}
	return number * factor, true
}

// Docker's CreatedSince column is a relative age, not an ISO date. Its values
// have rounded human units; compare those displayed ages rather than spelling.
func remoteCreatedAge(value string) (float64, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "less than a second") {
		return 0, true
	}
	value = strings.TrimPrefix(value, "about ")
	fields := strings.Fields(value)
	if len(fields) < 2 {
		return 0, false
	}
	number := float64(1)
	if fields[0] != "a" && fields[0] != "an" {
		var err error
		number, err = strconv.ParseFloat(fields[0], 64)
		if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
			return 0, false
		}
	}
	unit := strings.TrimSuffix(fields[1], "s")
	factor := float64(1)
	switch unit {
	case "second":
	case "minute":
		factor = 60
	case "hour":
		factor = 3600
	case "day":
		factor = 86400
	case "week":
		factor = 7 * 86400
	case "month":
		factor = 30 * 86400
	case "year":
		factor = 365 * 86400
	default:
		return 0, false
	}
	return number * factor, true
}
