package workbench

import (
	"reflect"
	"slices"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func chooseDisplayOrder(t testing.TB, state *State, order string) {
	t.Helper()
	state.Click(hitHostOrder)
	messages := state.messages.HostCatalog
	label := map[string]string{"manual": messages.Manual, "name": messages.Name, "ip": messages.IP}[order]
	clickPageMenuRow(t, state, label)
}

// canvasHasMenuRow reports whether an open page menu shows a row named label.
func canvasHasMenuRow(canvas *gfx.Canvas, label string) bool {
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			return true
		}
	}
	return false
}

// clickPageLabel clicks the page element that has the role and is named label,
// in the event pass of a paint, where the page reads it, and runs what the page
// deferred, as the runtime does after an event pass.
func clickPageLabel(t testing.TB, state *State, role gfx.Role, label string) {
	t.Helper()
	clickPageLabelIn(t, state, state.viewport, role, label)
}

// clickPageLabelIn is clickPageLabel in a window of the given size.
func clickPageLabelIn(t testing.TB, state *State, window gfx.Size, role gfx.Role, label string) {
	t.Helper()
	// A page taller than the window has controls below its fold, which a window
	// does not paint: the second try is a window as tall as a page.
	for _, size := range []gfx.Size{window, {Width: 1000, Height: 6000}} {
		if size.Width == 0 {
			size = gfx.Size{Width: 1400, Height: 900}
		}
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		var target gfx.Hit
		for _, hit := range canvas.Hits() {
			if hit.Role == role && hit.Label == label {
				target = hit
			}
		}
		if target.ID.IsZero() {
			continue
		}
		canvas = gfx.Canvas{}
		canvas.BeginEventPass(target.ID, gfx.ClickInfo{})
		state.Paint(&canvas, size)
		canvas.EndEventPass()
		canvas.RunAfterPaint()
		return
	}
	t.Fatalf("no %v named %q is painted", role, label)
}

// chooseMCPClient picks the client the assistant's export block shows, in the
// HTTP block or, with agent, the stdio one.
func chooseMCPClient(t testing.TB, state *State, agent bool, client aiMCPClient) {
	t.Helper()
	m := state.messages.AISettings
	title := m.MCPConfig
	if agent {
		title = m.AgentConfig
	}
	chooseSelectOption(t, state, title, aiMCPClientName(client))
}

// hasLabel reports whether the canvas paints a hit with the role named label.
func hasLabel(canvas *gfx.Canvas, label string, role gfx.Role) bool {
	for _, hit := range canvas.Hits() {
		if hit.Role == role && hit.Label == label {
			return true
		}
	}
	return false
}

// chooseSelectOption opens the page select named selectLabel and picks the option
// named optionLabel, as a user does.
func chooseSelectOption(t testing.TB, state *State, selectLabel, optionLabel string) {
	t.Helper()
	clickPageLabel(t, state, gfx.RoleCombobox, selectLabel)
	clickPageLabel(t, state, gfx.RoleOption, optionLabel)
}

// clickPageMenuRow chooses the row of the open page menu named label: the click
// is delivered in the event pass of a paint, where the page reads it, and what
// the page deferred runs after it, as the runtime does.
func clickPageMenuRow(t testing.TB, state *State, label string) {
	t.Helper()
	size := state.viewport
	if size.Width == 0 {
		size = gfx.Size{Width: 1400, Height: 900}
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	var row gfx.Hit
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleMenuItem && hit.Label == label {
			row = hit
		}
	}
	if row.ID.IsZero() {
		t.Fatalf("no menu row %q is painted", label)
	}
	canvas = gfx.Canvas{}
	canvas.BeginEventPass(row.ID, gfx.ClickInfo{})
	state.Paint(&canvas, size)
	canvas.EndEventPass()
	canvas.RunAfterPaint()
}

func displayHostIDs(state *State) []string {
	var ids []string
	for _, row := range state.hostTreeRows {
		if row.Kind == hostTreeHost {
			ids = append(ids, state.hosts[row.HostIndex].CatalogID)
		}
	}
	return ids
}

func displayOrderState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, freshOptions(t))
	state.hosts = []Host{
		{ID: 1, CatalogID: "host_z", Label: "zulu", Address: "192.168.1.10", Group: "Servers", Port: 22},
		{ID: 2, CatalogID: "host_b", Label: "Alpha", Address: "192.168.1.2", Group: "Servers", Port: 22},
		{ID: 3, CatalogID: "host_a", Label: "Alpha", Address: "2001:db8::10", Group: "Servers", Port: 22},
		{ID: 4, CatalogID: "host_c", Label: "alpha", Address: "2001:db8::2", Group: "Servers", Port: 22},
		{ID: 5, CatalogID: "host_d", Label: "Domain", Address: "z.example.test", Group: "Servers", Port: 22},
		{ID: 6, CatalogID: "host_e", Label: "Domain", Address: "A.example.test", Group: "Servers", Port: 22},
		{ID: 7, CatalogID: "host_f", Label: "Mapped", Address: "::ffff:192.168.1.1", Group: "Servers", Port: 22},
	}
	state.nextHostID = 8
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.hostDataChanged()
	state.save()
	return state
}

func TestHostDisplayOrderNameIsDeterministicAndPreservesManual(t *testing.T) {
	state := displayOrderState(t)
	original := slices.Clone(state.hosts)
	state.Click(hitHostCheckList.At(2))
	chooseDisplayOrder(t, state, "name")
	want := []string{"host_c", "host_a", "host_b", "host_d", "host_e", "host_f", "host_z"}
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("name order = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(state.hosts, original) || !state.hostSelected(2) {
		t.Fatal("display order rewrote the catalog or selection identity")
	}
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("reversed catalog changed display order: %v", got)
	}
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	chooseDisplayOrder(t, state, "manual")
	if got := displayHostIDs(state); !slices.Equal(got, []string{"host_z", "host_b", "host_a", "host_c", "host_d", "host_e", "host_f"}) {
		t.Fatalf("manual order not restored: %v", got)
	}
}

func TestHostDisplayOrderIPUsesNumericAddressesAndDomainFallback(t *testing.T) {
	state := displayOrderState(t)
	chooseDisplayOrder(t, state, "ip")
	want := []string{"host_f", "host_b", "host_z", "host_c", "host_a", "host_e", "host_d"}
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("IP order = %v, want %v", got, want)
	}
	slices.Reverse(state.hosts)
	state.hostDataChanged()
	if got := displayHostIDs(state); !slices.Equal(got, want) {
		t.Fatalf("IP order depends on input order: %v", got)
	}
	restarted := newTestState(t, state.options)
	if got := displayHostIDs(restarted); !slices.Equal(got, want) {
		t.Fatalf("IP order did not survive restart: %v", got)
	}
}

func TestHostDisplayOrderKeepsNestedSubtreesAndManualGroupMetadata(t *testing.T) {
	state := nestedHostGroupState(t)
	state.pinHostGroup("Team2")
	state.reorderHostGroup("Team/Prod", -1)
	chooseDisplayOrder(t, state, "name")
	var names []string
	for _, group := range state.hostGroups {
		names = append(names, group.Name)
	}
	if !slices.Equal(names, []string{"Team", "Team/Dev", "Team/Prod", "Team/Prod/EU", "Team2"}) {
		t.Fatalf("automatic group order = %v", names)
	}
	chooseDisplayOrder(t, state, "manual")
	if state.hostGroups[0].Name != "Team2" || state.hostGroupIndex("Team/Prod/EU") > state.hostGroupIndex("Team/Dev") {
		t.Fatal("automatic ordering changed pin/manual subtree order")
	}
}

func TestHostDisplayOrderWorksWithSearchAndGroupFilter(t *testing.T) {
	state := displayOrderState(t)
	chooseDisplayOrder(t, state, "name")
	typeSearch(state, "alpha")
	state.setHostFilter(true, "Servers")
	if got := displayHostIDs(state); !slices.Equal(got, []string{"host_c", "host_a", "host_b"}) {
		t.Fatalf("filtered order = %v", got)
	}
	state.Click(bulkAllHit())
	requireBulkSelection(t, state, 2, 3, 4)
}

func TestHostDisplayOrderHoldSwitchesToManual(t *testing.T) {
	state := displayOrderState(t)
	chooseDisplayOrder(t, state, "ip")
	if state.hostOrder != hostOrderIP {
		t.Fatal("automatic order did not apply")
	}
	beginHostSortCase(t, state)
	if state.hostOrder != hostOrderManual || !state.hostSort.enabled {
		t.Fatalf("holding a host under %v order did not switch to manual sorting", hostOrderIP)
	}
	state.endHostSort()
	// The automatic order still refuses a reorder move that reaches it stale.
	chooseDisplayOrder(t, state, "ip")
	before := slices.Clone(state.hosts)
	move := components.ReorderGridMove{ID: hitHostSortHandles.At(1), Target: hitHostSortHandles.At(2), From: 0, To: 1}
	state.applyHostSortMove(move)
	if !reflect.DeepEqual(state.hosts, before) {
		t.Fatal("stale reorder gesture rewrote an automatically ordered catalog")
	}
}

func TestHostDisplayOrderGroupManualActionsAreDisabled(t *testing.T) {
	state := nestedHostGroupState(t)
	chooseDisplayOrder(t, state, "name")
	state.Click(hitHostGroupMore.At(state.hostGroupIndex("Team/Dev")))
	canvas, _ := paintedTexts(state)
	for _, id := range []gfx.ID{hitGroupUp, hitGroupDown} {
		hit, ok := hitByID(canvas, id)
		if !ok || !hit.Disabled {
			t.Fatalf("manual group action %v is enabled under automatic order", id)
		}
	}
}

func TestHostDisplayOrderMenuIndicatesCurrentChoice(t *testing.T) {
	state := displayOrderState(t)
	messages := state.messages.HostCatalog
	for _, choice := range []struct{ name, label string }{{"name", messages.Name}, {"ip", messages.IP}, {"manual", messages.Manual}} {
		chooseDisplayOrder(t, state, choice.name)
		state.Click(hitHostOrder)
		canvas, _ := paintedTexts(state)
		for _, label := range []string{messages.Manual, messages.Name, messages.IP} {
			var row gfx.Hit
			for _, hit := range canvas.Hits() {
				if hit.Role == gfx.RoleMenuItem && hit.Label == label {
					row = hit
				}
			}
			if row.ID.IsZero() || row.Selected != (label == choice.label) {
				t.Fatalf("current %s menu item %q: %+v", choice.name, label, row)
			}
		}
		state.Click(hitHostOrder)
	}
}
