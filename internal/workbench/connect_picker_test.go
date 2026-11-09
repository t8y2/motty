package workbench

import (
	"fmt"
	"motty/internal/sshconn"
	"slices"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
)

func TestNewTabButtonAsksWhereToConnectAndMakesNoTabYet(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	state.Click(hitNewTab)
	if state.modal != modalCommandPalette || !state.commandPalette.connect {
		t.Fatalf("modal = %d connect = %v", state.modal, state.commandPalette.connect)
	}
	if got := len(state.deck.Workspaces(nil)); got != before {
		t.Fatalf("the tab was created before a choice: %d -> %d", before, got)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitCommandPaletteItems.At(0)); !ok {
		t.Fatal("the picker lists nothing")
	}
	// Closing it creates nothing either.
	state.Click(hitCommandPaletteClose)
	if state.modal != modalNone || len(state.deck.Workspaces(nil)) != before {
		t.Fatalf("after closing: modal %d tabs %d", state.modal, len(state.deck.Workspaces(nil)))
	}
	if state.commandPalette.connect {
		t.Fatal("the next palette would open as the picker")
	}
}

func TestPickingAHostCreatesANewTabForIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	host := state.hosts[0]
	state.Click(hitNewTab)
	// The picker groups the saved hosts, so the row of a host is named by the
	// host rather than by its place among all the rows.
	clickPickerRow(t, state, host.Label)
	if state.modal != modalNone || len(state.deck.Workspaces(nil)) != before+1 {
		t.Fatalf("modal %d, tabs %d -> %d", state.modal, before, len(state.deck.Workspaces(nil)))
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.HostID != host.ID || state.activeSection != sectionTerminal {
		t.Fatalf("active session %+v ok=%v section=%v", session, ok, state.activeSection)
	}
}

func TestNewTabWorkspaceTabLaunchesTemplatesAndCreatesWorkspaces(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	template := mustWorkspaceTemplate(t, "Operations", workspaceCatalogIDs(state, 0, 1), 2)
	state.templates = []WorkspaceTemplate{template}
	before := len(state.deck.Workspaces(nil))
	state.Click(hitNewTab)
	state.Click(hitConnectSortWorkspaces)
	if !state.commandPalette.workspaces || len(state.deck.Workspaces(nil)) != before {
		t.Fatal("switching to workspaces opened a tab")
	}
	clickPickerRow(t, state, template.Name)
	if state.modal != modalNone || len(state.deck.Workspaces(nil)) != before+1 || len(world.configs) != 2 {
		t.Fatalf("modal=%d tabs=%d dials=%d", state.modal, len(state.deck.Workspaces(nil)), len(world.configs))
	}
	state.Click(hitNewTab)
	state.Click(hitConnectSortWorkspaces)
	clickPickerRow(t, state, state.messages.WorkspaceNew)
	if state.modal != modalWorkspaceDraft || len(state.deck.Workspaces(nil)) != before+1 {
		t.Fatalf("new workspace modal=%d tabs=%d", state.modal, len(state.deck.Workspaces(nil)))
	}
}

func TestNewTabWorkspaceTabFiltersTemplatesAndKeepsHostSort(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.templates = []WorkspaceTemplate{mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0), 1)}
	state.Click(hitNewTab)
	state.Click(hitConnectSortRecent)
	state.Click(hitConnectSortWorkspaces)
	state.commandPalette.search.Insert("production")
	state.refreshCommandPalette()
	if got := state.commandPalette.visible; len(got) != 1 || state.commandPalette.entries[got[0]].target != hitConnectPickWorkspaces.At(0) {
		t.Fatalf("workspace matches = %v", got)
	}
	state.Click(hitConnectSortDefault)
	if state.recentConnectSort || state.commandPalette.workspaces {
		t.Fatal("default hosts mode was not restored")
	}
	state.Click(hitConnectSortWorkspaces)
	state.Click(hitConnectSortRecent)
	if !state.recentConnectSort || state.commandPalette.workspaces {
		t.Fatal("recent hosts mode was not restored")
	}
}

func TestNewTabWorkspaceTabOffersCreationWithoutTemplates(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	state.Click(hitConnectSortWorkspaces)
	if len(state.commandPalette.visible) != 1 || state.commandPalette.entries[state.commandPalette.visible[0]].target != hitConnectPickWorkspaceNew {
		t.Fatalf("empty workspaces offer = %+v", state.commandPalette.entries)
	}
	result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if !result.Handled || state.modal != modalWorkspaceDraft || len(state.deck.Workspaces(nil)) != 0 {
		t.Fatalf("keyboard new workspace: result=%+v modal=%d tabs=%d", result, state.modal, len(state.deck.Workspaces(nil)))
	}
}

func TestPickingThisComputerOpensALocalShellInANewTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	state.Click(hitNewTab)
	state.Click(hitCommandPaletteItems.At(0))
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.isLocal() || len(state.deck.Workspaces(nil)) != before+1 {
		t.Fatalf("local ok=%v tabs %d -> %d", ok, before, len(state.deck.Workspaces(nil)))
	}
}

func TestPickerSearchMatchesHostNamesAndAddresses(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	state.commandPalette.search.Insert(state.hosts[0].Address)
	state.refreshCommandPalette()
	palette := &state.commandPalette
	if len(palette.visible) == 0 {
		t.Fatal("searching a host's address finds nothing")
	}
	if got := palette.entries[palette.visible[0]].target; got != hitConnectPickList.At(int(state.hosts[0].ID)) {
		t.Fatalf("first match target = %d", got)
	}
	state.commandPalette.search.Insert("zzzz-no-such-host")
	state.refreshCommandPalette()
	if len(palette.visible) != 0 {
		t.Fatalf("%d matches for a nonsense query", len(palette.visible))
	}
}

func TestNewTabPickerMatchesEverySpaceSeparatedKeywordInBothSortModes(t *testing.T) {
	for _, recent := range []bool{false, true} {
		name := "default"
		if recent {
			name = "recent"
		}
		t.Run(name, func(t *testing.T) {
			options := testOptions(t)
			if err := saveState(options.StatePath, savedState{Hosts: []savedHost{
				{ID: "match", Label: "XX Production", Group: "Servers", Address: "oa.internal", Port: 22},
				{ID: "wrong-address", Label: "XX Staging", Group: "Servers", Address: "db.internal", Port: 22},
				{ID: "wrong-name", Label: "OA Production", Group: "Servers", Address: "db.internal", Port: 22},
			}}); err != nil {
				t.Fatal(err)
			}
			state := newTestState(t, options)
			state.Click(hitNewTab)
			if recent {
				state.Click(hitConnectSortRecent)
			}
			// Reverse the label/address order and vary case and whitespace. Every
			// term must match the same host, even across its displayed fields.
			state.commandPalette.search.Insert("  oA   Xx  ")
			want := []string{"XX Production"}
			if recent {
				want = []string{"Servers", "XX Production"}
			}
			if got := pickerRowLabels(state); !slices.Equal(got, want) {
				t.Fatalf("%s picker matches = %v", name, got)
			}
			if recent {
				state.Click(hitConnectSortDefault)
			} else {
				state.Click(hitConnectSortRecent)
			}
			if state.commandPalette.search.String() != "  oA   Xx  " {
				t.Fatal("switching sort cleared the search")
			}
			want = []string{"XX Production"}
			if !recent {
				want = []string{"Servers", "XX Production"}
			}
			if got := pickerRowLabels(state); !slices.Equal(got, want) {
				t.Fatalf("after switching sort, matches = %v", got)
			}
		})
	}
}

func TestTheCommandPaletteIsStillTheCommandPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	state.Click(hitCommandPaletteClose)
	state.openCommandPalette()
	if state.commandPalette.connect {
		t.Fatal("the palette opened as the picker")
	}
	found := false
	for index := 0; index < len(state.commandPalette.entries); index++ {
		found = found || state.commandPalette.entries[index].target == hitSettings
	}
	if !found {
		t.Fatal("the commands are missing from the palette")
	}
}

// clickPickerRow presses the picker row that shows label.
func clickPickerRow(t *testing.T, state *State, label string) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitCommandPaletteItems && hit.Label == label {
			state.Click(hit.ID)
			return
		}
	}
	t.Fatalf("the picker has no row for %q", label)
}

func TestPickerPencilOpensTheHostForEditing(t *testing.T) {
	state := newTestState(t, testOptions(t))
	before := len(state.deck.Workspaces(nil))
	host := state.hosts[0]
	state.Click(hitNewTab)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitConnectPickEdit.At(int(host.ID))); !ok {
		t.Fatal("the host's row has no pencil")
	}
	state.Click(hitConnectPickEdit.At(int(host.ID)))
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogEdit || state.hostDialogID != host.ID {
		t.Fatalf("modal %d mode %d id %d: the pencil must open host %d for editing", state.modal, state.hostDialogMode, state.hostDialogID, host.ID)
	}
	if len(state.deck.Workspaces(nil)) != before {
		t.Fatalf("tabs %d: opening the dialog must create no tab", len(state.deck.Workspaces(nil)))
	}
}

func TestNewTabPickerKeyboardStepsOverGroupHeadings(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitNewTab)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	// Local terminal, then a heading for Network and Router under it, so the
	// row after the first is a heading the keyboard must not land on.
	first := hitCommandPaletteItems.At(0)
	result := state.Key(gfx.KeyEvent{Target: first, Key: gfx.KeyDown})
	if !result.Handled || result.Focus != hitCommandPaletteItems.At(2) {
		t.Fatalf("down from the first row focused %v, want the row under the heading", result.Focus)
	}
	last := hitCommandPaletteItems.At(len(state.commandPalette.visible) - 1)
	result = state.Key(gfx.KeyEvent{Target: result.Focus, Key: gfx.KeyEnd})
	if !result.Handled || result.Focus != last {
		t.Fatalf("End focused %v, want the last choice %v", result.Focus, last)
	}
}

func TestNewTabPickerGroupsHostsAndTheWheelSurvivesTheNextPaint(t *testing.T) {
	options := testOptions(t)
	var hosts []savedHost
	for index := 0; index < 14; index++ {
		group := "Servers"
		if index%2 == 0 {
			group = "Lab"
		}
		hosts = append(hosts, savedHost{
			Label:   fmt.Sprintf("Host %02d", index),
			Group:   group,
			Address: fmt.Sprintf("host%02d.example.com", index),
			Port:    22,
		})
	}
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.Click(hitNewTab)

	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	texts := map[string]bool{}
	for _, text := range canvas.Texts() {
		texts[text.Value] = true
	}
	for _, group := range []string{"Lab", "Servers"} {
		if !texts[group] {
			t.Fatalf("the picker does not head its hosts with the group %q: %v", group, texts)
		}
	}
	first, ok := hitByID(&canvas, hitCommandPaletteItems.At(0))
	if !ok {
		t.Fatal("the picker paints no first row")
	}
	if result := state.Scroll(gfx.ScrollEvent{DeltaY: 120, Target: first.ID}); !result.Repaint || state.commandPalette.scroll == 0 {
		t.Fatalf("the wheel did not move the list: %+v, scroll %g", result, state.commandPalette.scroll)
	}
	top := state.commandPalette.scroll
	canvas.Reset()
	state.Paint(&canvas, size)
	if state.commandPalette.scroll != top {
		t.Fatalf("the next paint returned the list to offset %g, want it to stay at %g", state.commandPalette.scroll, top)
	}
	after, ok := hitByID(&canvas, hitCommandPaletteItems.At(int(top/commandPaletteRowStep)))
	if !ok || after.Label == first.Label {
		t.Fatalf("the wheel did not change the first row: %q -> %q", first.Label, after.Label)
	}
}

func TestRecentPickerShowsSuccessfulHostBeforeLocalAndKeepsDefaultOrder(t *testing.T) {
	options := testOptions(t)
	connection := &fakeConn{}
	options.Dial = func(sshconn.Config) Connection { return connection }
	state := newTestState(t, options)
	host := state.hosts[0]
	state.Click(hitNewTab)
	clickPickerRow(t, state, host.Label)
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	state.Click(hitNewTab)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	var recent gfx.ID
	for _, hit := range canvas.Hits() {
		if hit.Label == "Recent login" {
			recent = hit.ID
		}
	}
	if recent.IsZero() {
		t.Fatal("new-tab picker has no Recent login control")
	}
	state.Click(recent)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	first, ok := hitByID(&canvas, hitCommandPaletteItems.At(1)) // group title precedes the host
	if !ok || first.Label != host.Label {
		t.Fatalf("recent first host = %q, want %q", first.Label, host.Label)
	}
	// The mode and success survive a restart, then Default restores local first.
	second := newTestState(t, options)
	second.Click(hitNewTab)
	canvas.Reset()
	second.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	first, ok = hitByID(&canvas, hitCommandPaletteItems.At(1))
	if !ok || first.Label != host.Label {
		t.Fatalf("recent after restart = %q, want %q", first.Label, host.Label)
	}
	second.Click(hitConnectSortDefault)
	canvas.Reset()
	second.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	first, ok = hitByID(&canvas, hitCommandPaletteItems.At(0))
	if !ok || first.Label != second.messages.LocalTerminal {
		t.Fatalf("default first = %q", first.Label)
	}
}

// pickerRowLabels reads the actual painted rows, including non-selectable headings.
func pickerRowLabels(state *State) []string {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	var labels []string
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() == hitCommandPaletteItems || hit.ID.Scope() == hitCommandPaletteHeaders {
			labels = append(labels, hit.Label)
		}
	}
	return labels
}

func TestRecentPickerFiltersThenGroupsAndKeepsAnOpenListStill(t *testing.T) {
	world, options := newWorld(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	options.WallNow = func() time.Time { return now }
	if err := saveState(options.StatePath, savedState{
		RecentConnectSort: true,
		Hosts: []savedHost{
			{ID: "a1", Label: "node A1", Group: "a", Address: "a1.example", Port: 22},
			{ID: "a2", Label: "node A2", Group: "a", Address: "a2.example", Port: 22},
			{ID: "b1", Label: "node B1", Group: "b", Address: "b1.example", Port: 22},
			{ID: "b2", Label: "node B2", Group: "b", Address: "b2.example", Port: 22},
		},
		RecentConnections: map[string]time.Time{"a1": now.Add(-time.Minute), "b1": now.Add(-time.Hour), "a2": now.Add(-time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.Click(hitNewTab)
	want := []string{"a", "node A1", "node A2", "b", "node B1", state.messages.LocalTerminal, "b", "node B2"}
	// Equal timestamps use default order: A2 stays before B1.
	if got := pickerRowLabels(state); !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	// A successful dial while the picker is open must not rebuild or retime it.
	clickPickerRow(t, state, "node B2")
	state.Click(hitNewTab)
	before := pickerRowLabels(state)
	now = now.Add(48 * time.Hour)
	world.conns[0].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	if got := pickerRowLabels(state); !slices.Equal(got, before) {
		t.Fatalf("open list moved: %v -> %v", before, got)
	}
	state.commandPalette.search.Insert("node")
	state.refreshCommandPalette()
	want = []string{"b", "node B2", "a", "node A1", "node A2", "b", "node B1"}
	if got := pickerRowLabels(state); !slices.Equal(got, want) {
		t.Fatalf("search rows = %v, want %v", got, want)
	}
	state.Click(hitConnectSortDefault)
	if state.commandPalette.search.String() != "node" {
		t.Fatal("switch cleared search")
	}
	if got := pickerRowLabels(state); !slices.Equal(got, []string{"node A1", "node A2", "node B1", "node B2"}) {
		t.Fatalf("default search = %v", got)
	}
	state.Click(hitConnectSortRecent)
	result := state.Key(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyEnter})
	if !result.Handled || state.modal != modalNone {
		t.Fatalf("Enter did not select first connection: %+v", result)
	}
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if session.host.Label != "node B2" {
		t.Fatalf("selected %q", session.Title)
	}
}

func TestRecentPickerRepeatsGroupHeadingsAfterGlobalSort(t *testing.T) {
	options := testOptions(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	options.WallNow = func() time.Time { return now }
	if err := saveState(options.StatePath, savedState{
		RecentConnectSort: true,
		Hosts: []savedHost{
			{ID: "a1", Label: "A1", Group: "prod/a", Address: "a1.example", Port: 22},
			{ID: "a2", Label: "A2", Group: "prod/a", Address: "a2.example", Port: 22},
			{ID: "b", Label: "B", Group: "b", Address: "b.example", Port: 22},
		},
		RecentConnections: map[string]time.Time{"a1": now, "b": now.Add(-time.Minute), "a2": now.Add(-time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	state.Click(hitNewTab)
	want := []string{"prod/a", "A1", "b", "B", "prod/a", "A2", state.messages.LocalTerminal}
	if got := pickerRowLabels(state); !slices.Equal(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	canvas, texts := paintedTexts(state)
	for _, want := range []string{"Just now", "1m ago", "1h ago", "Never logged in"} {
		if !slices.Contains(texts, want) {
			t.Fatalf("missing %q in %v", want, texts)
		}
	}
	hit, ok := hitByID(canvas, hitCommandPaletteItems.At(1))
	if !ok || hit.Hint != now.Local().Format("2006-01-02 15:04:05 MST") {
		t.Fatalf("timestamp hint = %q", hit.Hint)
	}
}
