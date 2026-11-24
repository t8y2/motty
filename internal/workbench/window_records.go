package workbench

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"motty/internal/buildinfo"
	"motty/internal/configstore"
)

// windowRecordPrefix is the name every window's own local value starts with. A
// development build keeps its windows apart from an installed copy's, the way
// the rest of its data is kept apart.
func windowRecordPrefix() string {
	if buildinfo.Development {
		return "dev-window-"
	}
	return "window-"
}

// RestoreWindowCount compacts the records a previous run left open into 1..n and
// reports n, which is how many windows this run opens. A window the user closed
// leaves a gap; closing it keeps a window's identity its ordinal, so no window
// opens where none was. One window opens when nothing was recorded.
func RestoreWindowCount(path string) (int, error) {
	store, err := configstore.OpenShared(path, configFormat)
	if err != nil {
		return 1, err
	}
	defer store.Close()
	_, local, err := store.Load()
	if err != nil {
		return 1, err
	}
	prefix := windowRecordPrefix()
	var ids []int
	for key := range local {
		number, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		id, err := strconv.Atoi(number)
		if err != nil || id < 1 {
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 1, nil
	}
	sort.Ints(ids)
	count := len(ids)
	change := configstore.Change{Writer: configWriter(), Local: map[string][]byte{}}
	// Lowest first, so a record always moves into a number no window holds any
	// more. Every value is read from the map this load returned, not from the
	// writes below, so moving 2 into 1 and 3 into 2 does not move 1's own tab.
	for index, id := range ids {
		want := index + 1
		if id != want {
			change.Local[prefix+strconv.Itoa(want)] = local[prefix+strconv.Itoa(id)]
		}
	}
	// A record past n belongs to no window of this run.
	for _, id := range ids {
		if id > count {
			change.Local[prefix+strconv.Itoa(id)] = nil
		}
	}
	if len(change.Local) > 0 {
		if _, err := store.Commit(change); err != nil {
			return count, err
		}
	}
	return count, nil
}

// DropWindowRecord removes this window's own saved state, so a window the user
// closed does not come back at the next launch. Queue it if the key is locked
// or a migration has exclusive access to the store.
func (state *State) DropWindowRecord() {
	if state.configBase == nil {
		return
	}
	name := state.configBase.windowName
	if name == "" || state.options.StatePath == "" {
		return
	}
	p := state.appProtection
	if p.droppedWindows == nil {
		p.droppedWindows = make(map[string][]byte)
	}
	p.droppedWindows[name] = nil // configstore deletes local records with a nil value
	if !state.appAccessBlocked() {
		state.flushDroppedWindowRecords()
	}
}

func (state *State) flushDroppedWindowRecords() {
	drops := state.appProtection.droppedWindows
	if len(drops) == 0 {
		return
	}
	store, err := configstore.OpenShared(state.options.StatePath, configFormat)
	if err != nil {
		state.logWindowRecordError(err)
		return
	}
	defer store.Close()
	if _, err := store.Commit(configstore.Change{Writer: configWriter(), Local: drops}); err != nil {
		state.logWindowRecordError(err)
		return
	}
	clear(drops)
}

// A stale window can return at the next launch, so log a failed deletion.
func (state *State) logWindowRecordError(err error) {
	if state.options.Log == nil {
		return
	}
	fmt.Fprintf(state.options.Log, "motty: delete window records: %v\n", err)
}
