package workbench

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestToolbarDefaultsAndAllOffSurvivePersistence(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.toolbars.snapshot() != nil {
		t.Fatal("untouched toolbars should retain the existing defaults")
	}
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		for _, entry := range state.toolbars.entries(group) {
			// The latency readout owns its own switch and starts hidden.
			want := !(group == toolbarTerminal && entry.Key == "latency")
			if entry.Enabled != want {
				t.Fatalf("default entry %q enabled=%v, want %v", entry.Key, entry.Enabled, want)
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
	prefs.restore(&decoded)
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
	entries := normalizeToolbar(toolbarWindow, []toolbarEntry{{Key: "settings", Enabled: false}, {Key: "settings", Enabled: true}, {Key: "removed-entry", Enabled: true}})
	if len(entries) != len(toolbarCatalog[toolbarWindow]) || entries[0].Key != "settings" || entries[0].Enabled {
		t.Fatal("invalid entries changed a known preference")
	}
	for _, entry := range entries[1:] {
		if !entry.Enabled {
			t.Fatal("new/missing entries should follow current defaults")
		}
	}
}

func TestToolbarRestoreReturnsTheLatencyReadoutToTheRailTop(t *testing.T) {
	entries := normalizeToolbar(toolbarTerminal, []toolbarEntry{{Key: "notes", Enabled: true}, {Key: "files", Enabled: false}})
	if len(entries) != len(toolbarCatalog[toolbarTerminal]) || entries[0].Key != "latency" || entries[0].Enabled {
		t.Fatalf("migrated terminal layout = %+v", entries)
	}

	// A saved dock keeps the side the user's own terminal tools use.
	var prefs toolbarPreferences
	prefs.restore(&savedToolbars{
		Terminal: []toolbarEntry{{Key: "notes", Enabled: true}},
		Dock:     []toolbarDockEntry{{toolbarTerminal, "notes", toolbarDockLeftBottom}},
	})
	if prefs.layouts[toolbarTerminal][0].Key != "latency" {
		t.Fatalf("layout did not lead with the readout: %+v", prefs.layouts[toolbarTerminal])
	}
	first := prefs.dock[0]
	if first.Group != toolbarTerminal || first.Key != "latency" || first.Zone != toolbarDockLeftTop {
		t.Fatalf("migrated dock = %+v", prefs.dock)
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	state.toolbars.restore(saved.Toolbars)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, pair := range []struct {
		first, second gfx.ID
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
		"notes", "close-pane", "broadcast", "snippets",
		"split-horizontal", "split-vertical",
		"record", "export-recording", "save-output", "export-log",
		"resources", "files", "local-proxy", "file-transfer", "remote-tools",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("terminal toolbar order = %v, want %v", got, want)
	}
}

func TestToolbarSettingsPaintReusesItsLabelsAndRows(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas gfx.Canvas
	for group := toolbarWindow; group <= toolbarSFTP; group++ {
		state.openToolbarSettings(group)
		paint := func() {
			canvas.Reset()
			state.Paint(&canvas, gfx.Size{Width: 1200, Height: 900})
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
	size := gfx.Size{Width: 1200, Height: 800}
	var canvas gfx.Canvas
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
