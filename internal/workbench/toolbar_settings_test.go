package workbench

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

func TestToolbarDefaultsAndAllOffSurvivePersistence(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.toolbars.snapshot() != nil {
		t.Fatal("untouched toolbars should retain the existing defaults")
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		for _, entry := range state.toolbars.entries(group) {
			wantEnabled := entry.Key != "latency"
			if entry.Enabled != wantEnabled {
				t.Fatalf("default entry %q enabled=%v, want %v", entry.Key, entry.Enabled, wantEnabled)
			}
		}
		state.openToolbarSettings(group)
		state.clickToolbarSettings(hitToolbarDisableAll)
	}
	want := state.toolbars.snapshot()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var decoded savedToolbars
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	var prefs toolbarPreferences
	prefs.restore(&decoded, terminalToolbarRight)
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		if len(prefs.entries(group)) != len(toolbarCatalog[group]) {
			t.Fatal("all-off was confused with an absent/default setting")
		}
		for _, entry := range prefs.entries(group) {
			if entry.Enabled {
				t.Fatal("restart restored a disabled entry")
			}
		}
	}
	got, _, err := loadState(state.options.StatePath)
	if err != nil || !reflect.DeepEqual(got.Toolbars, want) {
		t.Fatalf("disk round trip: %+v %v", got.Toolbars, err)
	}
	state.toolbars.customize(toolbarWindow)[0].Enabled = true
	if want.Window[0].Enabled {
		t.Fatal("saved snapshots alias the live editable order")
	}
}

func TestToolbarRestoreIgnoresUnknownAndDuplicateKeys(t *testing.T) {
	entries := normalizeToolbar(toolbarWindow, []toolbarEntry{{Key: "settings", Enabled: false}, {Key: "settings", Enabled: true}, {Key: "removed-entry", Enabled: true}}, terminalToolbarRight)
	if len(entries) != len(toolbarCatalog[toolbarWindow]) || entries[0].Key != "settings" || entries[0].Enabled {
		t.Fatal("invalid entries changed a known preference")
	}
	for index, entry := range entries {
		if index > 0 && !entry.Enabled {
			t.Fatal("new/missing entries should follow current defaults")
		}
		if entry.Side != "" {
			t.Fatalf("window toolbar entry %q persisted a terminal-only side", entry.Key)
		}
	}
}

func TestLegacyTerminalToolbarPositionMigratesToPerActionSides(t *testing.T) {
	var prefs toolbarPreferences
	prefs.restore(nil, terminalToolbarLeft)
	entries := prefs.entries(toolbarTerminal)
	if len(entries) != len(toolbarCatalog[toolbarTerminal]) {
		t.Fatalf("migrated terminal toolbar has %d entries, want %d", len(entries), len(toolbarCatalog[toolbarTerminal]))
	}
	for _, entry := range entries {
		if terminalToolbarSideFromString(entry.Side, terminalToolbarRight) != terminalToolbarLeft {
			t.Fatalf("legacy left toolbar entry %q migrated to %q", entry.Key, entry.Side)
		}
	}
}

func TestLegacyLatencySwitchMigratesAndConnectionColorDefaultsOn(t *testing.T) {
	state := newTestState(t, testOptions(t))
	saved := savedState{
		TerminalShowLatency: true,
		Toolbars: &savedToolbars{Terminal: []toolbarEntry{{
			Key: "close-pane", Side: terminalToolbarRight.String(), Enabled: false,
		}}},
	}
	state.toolbars.restore(saved.Toolbars, terminalToolbarRight)
	state.restoreTerminalStatusEntries(saved)

	entries := state.toolbars.entries(toolbarTerminal)
	if len(entries) < 2 || entries[0].Key != "connection-color" || entries[1].Key != "latency" {
		t.Fatalf("migrated terminal status order = %+v, want connection color then latency", entries)
	}
	if !entries[0].Enabled || !entries[1].Enabled {
		t.Fatalf("migrated terminal status enabled color=%v latency=%v", entries[0].Enabled, entries[1].Enabled)
	}
	if state.terminalHideConnectionColor || !state.terminalShowLatency {
		t.Fatalf("migrated runtime status colorOff=%v latency=%v", state.terminalHideConnectionColor, state.terminalShowLatency)
	}
}

func TestToolbarOrderAndResetAreRestoredByANewState(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		state.openToolbarSettings(group)
		first := state.toolbars.entries(group)[0].Key
		state.clickToolbarSettings(hitToolbarToggles.At(toolbarEntryIndex(group, first)))
		state.moveToolbarEntry(components.ReorderMove{
			ID: hitToolbarHandles.At(toolbarEntryIndex(group, first)), From: 0, To: len(toolbarCatalog[group]) - 1,
		})
	}
	want := state.toolbars.snapshot()
	reopened := newTestState(t, options)
	if !reflect.DeepEqual(reopened.toolbars.snapshot(), want) {
		t.Fatal("starting a new state did not restore the saved order and hidden entries")
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		reopened.openToolbarSettings(group)
		reopened.clickToolbarSettings(hitToolbarReset)
	}
	reset := newTestState(t, options)
	if reset.toolbars.snapshot() != nil {
		t.Fatal("reset was not persisted as current defaults")
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		if !slices.Equal(reset.toolbars.entries(group), toolbarDefaults[group]) {
			t.Fatal("a restart after reset kept a custom order or hidden entry")
		}
	}
}

func TestToolbarAllOffHidesOnlyEntriesAndResetRestoresThem(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitWindowSettings); !ok {
		t.Fatal("window toolbar fixture was not shown")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPRefresh)); !ok {
		t.Fatal("SFTP toolbar fixture was not shown")
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		state.openToolbarSettings(group)
		state.clickToolbarSettings(hitToolbarDisableAll)
	}
	state.closeModal()
	canvas.Reset()
	state.Paint(&canvas, size)
	for group, catalog := range toolbarCatalog {
		for _, spec := range catalog {
			id := spec.id
			if toolbarGroup(group) == toolbarSFTP {
				id = contextID(id)
			}
			if _, ok := hitByID(&canvas, id); ok {
				t.Fatalf("hidden toolbar entry %s is still interactive", spec.key)
			}
		}
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPUp)); !ok {
		t.Fatal("configuring the header removed path navigation")
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		state.openToolbarSettings(group)
		state.clickToolbarSettings(hitToolbarReset)
	}
	state.closeModal()
	canvas.Reset()
	state.Paint(&canvas, size)
	if _, ok := hitByID(&canvas, hitWindowSettings); !ok {
		t.Fatal("reset did not restore the window entry")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPRefresh)); !ok {
		t.Fatal("reset did not restore the SFTP entry")
	}
}

func TestToolbarCustomOrderAppearsInTheThreeRealToolbars(t *testing.T) {
	state, _, _, _ := openTestSFTP(t)
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		slices.Reverse(state.toolbars.customize(group))
	}
	state.save()
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	state.toolbars.restore(saved.Toolbars, terminalToolbarRight)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, pair := range []struct {
		first, second ui.ID
		horizontal    bool
	}{
		{hitWindowSettings, hitTransferCenter, true},
		{hitSFTPOpen, hitConnExport, false},
		{contextID(hitSFTPClose), contextID(hitSFTPUpload), true},
	} {
		first, firstOK := hitByID(&canvas, pair.first)
		second, secondOK := hitByID(&canvas, pair.second)
		wrongOrder := first.Bounds.Y >= second.Bounds.Y
		if pair.horizontal {
			wrongOrder = first.Bounds.X >= second.Bounds.X
		}
		if !firstOK || !secondOK || wrongOrder {
			t.Fatalf("saved order was not applied: %+v %+v", first, second)
		}
	}
	if _, ok := hitByID(&canvas, hitBroadcast); ok {
		t.Fatal("customizing the toolbar invented an unavailable split-pane action")
	}
	if hit, ok := hitByID(&canvas, hitTerminalRecordingExport); !ok || !hit.Disabled {
		t.Fatal("reordering enabled an unavailable recording export")
	}
}

func TestTerminalToolbarDefaultOrderMatchesTheRail(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var got []string
	for _, entry := range state.toolbars.entries(toolbarTerminal) {
		if entry.Enabled {
			got = append(got, entry.Key)
		}
	}
	want := []string{
		"connection-color",
		"close-pane", "broadcast",
		"split-horizontal", "split-vertical",
		"record", "export-recording", "save-output", "export-log",
		"resources", "files", "file-transfer", "remote-tools",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("terminal toolbar order = %v, want %v", got, want)
	}
}

func TestTerminalToolbarTwoColumnOrderAndVisibilityPersist(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	moveToolbarEntryToSide(state, "connection-color", terminalToolbarLeft, 0)
	moveToolbarEntryToSide(state, "files", terminalToolbarLeft, 1)
	moveToolbarEntryToSide(state, "latency", terminalToolbarLeft, 2)
	state.toggleTerminalStatusEntry("connection-color")
	state.toggleTerminalStatusEntry("latency")
	state.save()

	restarted := newTestState(t, options)
	var left []string
	for _, entry := range restarted.toolbars.entries(toolbarTerminal) {
		if terminalToolbarSideFromString(entry.Side, terminalToolbarRight) == terminalToolbarLeft {
			left = append(left, entry.Key)
		}
	}
	want := []string{"connection-color", "files", "latency"}
	if !slices.Equal(left, want) {
		t.Fatalf("saved left column = %v, want %v", left, want)
	}
	if !restarted.terminalHideConnectionColor || !restarted.terminalShowLatency {
		t.Fatalf("saved status entries colorOff=%v latency=%v",
			restarted.terminalHideConnectionColor, restarted.terminalShowLatency)
	}
}

func TestTerminalToolbarDropDoesNotLeaveAKeyboardFocusBackground(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.openToolbarSettings(toolbarTerminal)
	canvas, _ := paintAt(state, ui.Size{Width: 1200, Height: 900})
	handle := hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, "connection-color"))
	hit, ok := hitByID(canvas, handle)
	if !ok {
		t.Fatal("connection-color drag handle is missing")
	}
	x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
	state.terminalToolbarSettingsPointer(ui.PointerEvent{
		Kind: ui.PointerDown, Target: handle, Button: ui.PointerButtonPrimary, X: x, Y: y,
	})
	drop := state.toolbarEditor.zoneViewport[terminalToolbarZoneLeftBottom]
	state.terminalToolbarSettingsPointer(ui.PointerEvent{
		Kind: ui.PointerMove, Target: handle, X: drop.X + 8, Y: drop.Y + 8,
	})
	result := state.terminalToolbarSettingsPointer(ui.PointerEvent{
		Kind: ui.PointerUp, Target: handle, X: drop.X + 8, Y: drop.Y + 8,
	})
	if !result.Handled || result.FocusVisible {
		t.Fatalf("drop result=%+v, want handled without a persistent focus ring", result)
	}
}

func TestTerminalToolbarSameZoneDropUsesRemovedIndex(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.openToolbarSettings(toolbarTerminal)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1200, Height: 900})

	keys := state.toolbarEditor.zoneKeys[terminalToolbarZoneRightTop]
	if len(keys) < 2 || keys[0] != "connection-color" {
		t.Fatalf("default right-top order = %v", keys)
	}
	drag := terminalToolbarDrag{
		key: keys[0], zone: terminalToolbarZoneRightTop,
		dropZone: terminalToolbarZoneRightTop, dropIndex: 1, hasDrop: true,
	}
	if got := state.toolbarEditor.terminalToolbarDropSlot(drag); got != 0 {
		t.Fatalf("same-zone slot=%d, want 0 after removing the source", got)
	}
	if moveToolbarEntryToZone(state, drag.key, drag.dropZone, 0) {
		t.Fatal("dropping an item immediately after itself changed the order")
	}
}

func TestTerminalToolbarDropIndexUsesPaintedRowMidpoints(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.openToolbarSettings(toolbarTerminal)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1200, Height: 900})

	zone := terminalToolbarZoneRightTop
	keys := state.toolbarEditor.zoneKeys[zone]
	if len(keys) < 2 {
		t.Fatalf("right-top keys=%v", keys)
	}
	list := &state.toolbarEditor.zoneLists[zone]
	first, ok := list.RowBounds(hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, keys[0])))
	if !ok {
		t.Fatal("first row has no painted bounds")
	}
	if got := state.toolbarEditor.terminalToolbarDropIndex(zone, first.Y+first.Height/2); got != 1 {
		t.Fatalf("first row midpoint slot=%d, want 1", got)
	}
	last, ok := list.RowBounds(hitToolbarHandles.At(toolbarEntryIndex(toolbarTerminal, keys[len(keys)-1])))
	if !ok {
		t.Fatal("last row has no painted bounds")
	}
	if got := state.toolbarEditor.terminalToolbarDropIndex(zone, last.Y+last.Height/2); got != len(keys) {
		t.Fatalf("last row midpoint slot=%d, want append at %d", got, len(keys))
	}
}

func TestToolbarSettingsPaintReusesItsLabelsAndRows(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas ui.Canvas
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		state.openToolbarSettings(group)
		paint := func() {
			canvas.Reset()
			state.Paint(&canvas, ui.Size{Width: 1200, Height: 900})
		}
		paint()
		if allocations := testing.AllocsPerRun(100, paint); allocations != 0 {
			t.Fatalf("toolbar group %d paint allocated %v times", group, allocations)
		}
	}
}

// Hiding and showing window entries changes how much room the tabs get, so the
// geometry cached for the old button count must not outlive the change.
func TestWindowToolbarChangesRelayoutTheTabStrip(t *testing.T) {
	state, _ := manyTabs(t, 30)
	size := ui.Size{Width: 1200, Height: 800}
	var canvas ui.Canvas
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	// Hide every window entry, then paint so the bar caches the room it has with
	// no trailing buttons.
	state.openToolbarSettings(toolbarWindow)
	state.clickToolbarSettings(hitToolbarDisableAll)
	state.closeModal()
	paint()

	// Show them again without resizing: the tabs and the new-tab button must give
	// the room back instead of keeping the cached, wider area.
	state.openToolbarSettings(toolbarWindow)
	state.clickToolbarSettings(hitToolbarEnableAll)
	state.closeModal()
	paint()

	first, ok := hitByID(&canvas, hitTransferCenter)
	if !ok {
		t.Fatal("the first window entry is not painted after enabling it")
	}
	plus, ok := hitByID(&canvas, hitNewTab)
	if !ok {
		t.Fatal("the new-tab button is not painted")
	}
	if plus.Bounds.X+plus.Bounds.Width > first.Bounds.X {
		t.Fatalf("the new-tab button %+v runs under the enabled buttons from %+v", plus.Bounds, first.Bounds)
	}
	for _, tab := range state.workspaceTabs {
		if hit, ok := hitByID(&canvas, hitTabList.At(int(tab.ID))); ok && hit.Bounds.X+hit.Bounds.Width > first.Bounds.X {
			t.Fatalf("tab %d (%+v) runs under the enabled buttons from %+v", tab.ID, hit.Bounds, first.Bounds)
		}
	}
}
