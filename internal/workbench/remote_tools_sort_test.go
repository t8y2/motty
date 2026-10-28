package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"motty/internal/remotetools"
)

func TestRemoteProcessColumnsSortByTheirDisplayedTypes(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.procs = []remotetools.Process{
		{PID: 2, User: "zoe", CPU: 2, Memory: 9, Elapsed: "2:00", Command: "/z"},
		{PID: 10, User: "Alice", CPU: 12, Memory: 3, Elapsed: "1-00:00:00", Command: "/a"},
		{PID: 100, User: "bob", CPU: 9, Memory: 12, Elapsed: "01:02:03", Command: "/b"},
	}
	state.remote.selected = "10"
	for column, want := range [][2]string{{"2", "100"}, {"10", "2"}, {"2", "10"}, {"10", "100"}, {"2", "10"}, {"10", "2"}} {
		for i, direction := range []components.TableSortDirection{components.TableSortAscending, components.TableSortDescending} {
			state.remote.ordering[remoteProcesses] = components.TableSortState{Column: column, Direction: direction}
			state.rebuildRemoteRows()
			if state.remote.rows[0].key != want[i] {
				t.Fatalf("column=%d direction=%v: first=%v want=%s", column, direction, state.remote.rows[0], want[i])
			}
			if selected, ok := state.selectedProcess(); !ok || selected.PID != 10 {
				t.Fatal("sorting changed the selected operation target")
			}
		}
	}
}

func TestRemoteImageSizeAndCreationSortNumericallyWithMissingValuesLast(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.tab = remoteImages
	state.remote.resources = []remotetools.Resource{
		{Key: "a", Cells: []string{"a", "aa", "9GB", "2 weeks ago"}},
		{Key: "b", Cells: []string{"b", "bb", "800MB", "3 months ago"}},
		{Key: "c", Cells: []string{"c", "cc", "1.5GB", "About a minute ago"}},
		{Key: "missing", Cells: []string{"missing", "zz", "N/A", "N/A"}},
	}
	for _, test := range []struct {
		column    int
		direction components.TableSortDirection
		first     string
	}{
		{2, components.TableSortAscending, "b"}, {2, components.TableSortDescending, "a"},
		{3, components.TableSortAscending, "b"}, {3, components.TableSortDescending, "c"},
	} {
		state.remote.ordering[remoteImages] = components.TableSortState{Column: test.column, Direction: test.direction}
		state.rebuildRemoteRows()
		if state.remote.rows[0].key != test.first || state.remote.rows[3].key != "missing" {
			t.Fatalf("%+v: rows=%+v", test, state.remote.rows)
		}
	}
}

func TestRemoteSortPrecedesPaginationAndKeepsConfirmationTarget(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.remote.procs = nil
	for i := 0; i < 30; i++ {
		state.remote.procs = append(state.remote.procs, remotetools.Process{PID: 1000 - i, User: "same", CPU: float64(i), Elapsed: "00:01", Command: "worker"})
	}
	state.rebuildRemoteRows()
	state.remote.selected, state.remote.page = "1000", 2
	state.Click(remoteTableHeaderID(remoteProcesses, 0))
	if state.remote.rows[0].key != "971" || state.remote.page != 0 {
		t.Fatal("PID sorted lexically, sorted only a page, or did not return to the first page")
	}
	state.Click(hitRemoteKill)
	if state.modal != modalForm || !strings.Contains(state.form.description, "1000") {
		t.Fatal("confirmation targeted a row index instead of the selected PID")
	}
	state.Click(hitFormCancel)
}

func TestRemoteSortSurvivesRefreshAndIsRememberedForEachTab(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	state.Click(remoteTableHeaderID(remoteProcesses, 1))
	state.Click(remoteTableHeaderID(remoteProcesses, 1)) // username descending
	selectedOrder := state.remote.tableSort()
	state.Click(hitRemoteIntervals.At(0))
	state.tickRemoteRefresh(5 * time.Second)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if state.remote.tableSort() != selectedOrder || state.remote.rows[0].key != "2211" {
		t.Fatal("auto refresh reset the selected column or direction")
	}
	state.Click(hitRemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading })
	for column := 0; column < remoteColumnCounts[remoteContainers]; column++ {
		state.Click(remoteTableHeaderID(remoteContainers, column))
		if order := state.remote.tableSort(); order.Column != column || order.Direction != components.TableSortAscending {
			t.Fatal("Docker header did not start in ascending order")
		}
		state.Click(remoteTableHeaderID(remoteContainers, column))
		if state.remote.tableSort().Direction != components.TableSortDescending {
			t.Fatal("Docker header did not switch to descending")
		}
	}
	state.Click(hitRemoteTabProcesses)
	waitFor(t, state, func() bool { return !state.remote.loading })
	if state.remote.tableSort() != selectedOrder {
		t.Fatal("switching tabs discarded the process table's ordering")
	}
}

func TestRemoteSortValueParsers(t *testing.T) {
	for _, test := range []struct {
		value string
		parse func(string) (float64, bool)
		want  float64
		valid bool
	}{
		{"10-02:03:04", remoteElapsedSeconds, 10*86400 + 2*3600 + 3*60 + 4, true},
		{"02:10", remoteElapsedSeconds, 130, true}, {"-", remoteElapsedSeconds, 0, false},
		{"1.5 GiB", remoteSizeBytes, 1.5 * (1 << 30), true}, {"900MB", remoteSizeBytes, 9e8, true}, {"N/A", remoteSizeBytes, 0, false},
		{"About an hour ago", remoteCreatedAge, 3600, true}, {"Less than a second ago", remoteCreatedAge, 0, true},
		{"3 months ago", remoteCreatedAge, 90 * 86400, true}, {"unknown", remoteCreatedAge, 0, false},
	} {
		t.Run(fmt.Sprintf("%s/%v", test.value, test.want), func(t *testing.T) {
			got, ok := test.parse(test.value)
			if ok != test.valid || ok && got != test.want {
				t.Fatalf("got %v,%v want %v,%v", got, ok, test.want, test.valid)
			}
		})
	}
}
