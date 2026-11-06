package workbench

import (
	"encoding/json"
	"testing"
)

// bastionHosts are two hosts reached at one address: a bastion host that each
// target behind it is entered through.
func bastionHosts() []savedHost {
	return []savedHost{
		{ID: "host_first", Label: "Target A", Address: "bastion.example.com", User: "ops", Port: 22},
		{ID: "host_second", Label: "Target B", Address: "bastion.example.com", User: "ops", Port: 22},
	}
}

func TestTabsOfHostsSharingADestinationReopenOnTheirOwnHost(t *testing.T) {
	options := testOptions(t)
	tab := func(label, catalogID string) savedSession {
		return savedSession{Label: label, Address: "bastion.example.com", User: "ops", Port: 22, SavedHost: true, CatalogID: catalogID}
	}
	err := saveState(options.StatePath, savedState{
		Hosts:         bastionHosts(),
		Sessions:      []savedSession{tab("Target B", "host_second"), tab("Target A", "host_first"), tab("Gone", "host_removed")},
		ActiveSession: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.sessions) != 2 {
		t.Fatalf("restored %d tabs, want the two whose hosts exist (a tab must not move to a neighbour)", len(state.sessions))
	}
	for index, want := range []string{"host_second", "host_first"} {
		if got := state.sessions[index].host.CatalogID; got != want {
			t.Errorf("tab %d reopened on %q, want %q", index, got, want)
		}
	}
	state.save()
	if saved := newTestState(t, options); len(saved.sessions) != 2 || saved.sessions[0].host.CatalogID != "host_second" {
		t.Fatalf("tabs lost their host across a save: %+v", saved.sessions)
	}
}

func TestTabSavedBeforeCatalogIDsIsFoundByDestination(t *testing.T) {
	options := testOptions(t)
	err := saveState(options.StatePath, savedState{
		Hosts:    []savedHost{{ID: "host_only", Label: "Only", Address: "solo.example.com", User: "ops", Port: 22}},
		Sessions: []savedSession{{Label: "Only", Address: "solo.example.com", User: "ops", Port: 22, SavedHost: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.sessions) != 1 || state.sessions[0].host.CatalogID != "host_only" {
		t.Fatalf("an older saved tab was not restored: %+v", state.sessions)
	}
}

func TestRouteEndsAreFoundByCatalogID(t *testing.T) {
	hosts := []Host{
		{ID: 1, CatalogID: "host_first", Label: "Target A", Address: "bastion.example.com", User: "ops", Port: 22},
		{ID: 2, CatalogID: "host_second", Label: "Target B", Address: "bastion.example.com", User: "ops", Port: 22},
		{ID: 3, CatalogID: "host_jump", Label: "Jump", Address: "jump.example.com", User: "ops", Port: 22},
	}
	route := func(targetID string) savedProxy {
		return savedProxy{
			TargetID: targetID, JumpID: "host_jump",
			TargetAddress: "bastion.example.com", TargetUser: "ops", TargetPort: 22,
			JumpAddress: "jump.example.com", JumpUser: "ops", JumpPort: 22,
		}
	}
	if routes := decodeProxies([]savedProxy{route("host_second")}, hosts); len(routes) != 1 || routes[0].TargetID != 2 {
		t.Fatalf("a route for the second host at a destination went to %+v", routes)
	}
	if routes := decodeProxies([]savedProxy{route("")}, hosts); len(routes) != 1 || routes[0].TargetID != 1 {
		t.Fatalf("a route saved without IDs was not found by destination: %+v", routes)
	}
	if routes := decodeProxies([]savedProxy{route("host_removed")}, hosts); len(routes) != 0 {
		t.Fatalf("a route whose target is gone moved to a neighbour: %+v", routes)
	}
}

func TestRouteRecordKeysOnlyChangeForLaterHostsAtADestination(t *testing.T) {
	key := func(proxy savedProxy) string {
		raw, err := json.Marshal(proxy)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		return proxyRecordKey(fields)
	}
	first := Host{Protocol: protocolSSH, Address: "bastion.example.com", User: "ops", Port: 22}
	route := savedProxy{TargetAddress: first.Address, TargetUser: first.User, TargetPort: first.Port}
	legacy := key(route)
	route.TargetID = catalogIDFor(first)
	if key(route) != legacy {
		t.Fatal("the key of a route to the first host at a destination changed; devices on older builds would see a second route")
	}
	route.TargetID = "host_second"
	if key(route) == legacy {
		t.Fatal("a route to a later host at the destination shares the first one's record")
	}
}

func TestCatalogIDsOfHostsAtOneDestination(t *testing.T) {
	a := Host{Label: "Target A", Address: "bastion.example.com", User: "ops", Port: 22}
	b := Host{Label: "target b", Address: "bastion.example.com", User: "ops", Port: 22}
	used := map[string]struct{}{}
	first := uniqueCatalogID("", a, used)
	second := uniqueCatalogID("", b, used)
	if first != catalogIDFor(a) {
		t.Fatalf("the first host at a destination lost its historical ID: %q", first)
	}
	if second == first {
		t.Fatal("two hosts share a catalog ID")
	}
	// A second device that adds the same named host behind the bastion, with the
	// first one already known, must arrive at the same ID.
	if again := uniqueCatalogID("", Host{Label: "Target B", Address: b.Address, User: b.User, Port: b.Port}, map[string]struct{}{first: {}}); again != second {
		t.Fatalf("the ID of a later host depends on more than its destination and name: %q vs %q", again, second)
	}
}

func TestEditingOneHostLeavesTheForwardsOfItsDestinationNeighbour(t *testing.T) {
	options := testOptions(t)
	err := saveState(options.StatePath, savedState{
		Hosts: bastionHosts(),
		Forwards: []savedForward{{
			Label: "db", Mode: "local", HostLabel: "Target A", HostAddress: "bastion.example.com", HostUser: "ops", HostPort: 22,
			LocalPort: 5432, DestinationHost: "db.internal", DestinationPort: 5432,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	index := state.hostIndex(state.hosts[1].ID)
	previous := state.hosts[index]
	if msg := state.hostReferenceError([]uint32{previous.ID}); msg != "" {
		t.Fatalf("deleting a host was refused because a neighbour at its destination has a forward: %q", msg)
	}
	updated := previous
	updated.Address, updated.Label = "other.example.com", "Target B"
	state.replaceHost(index, previous, updated)
	if got := state.forwards[0].HostAddress; got != "bastion.example.com" {
		t.Fatalf("a forward followed a host that moved away from its destination: %q", got)
	}
	// With no host left behind, the forward does follow the one that moved.
	state.hosts = state.hosts[:1]
	previous = state.hosts[0]
	updated = previous
	updated.Address = "moved.example.com"
	state.replaceHost(0, previous, updated)
	if got := state.forwards[0].HostAddress; got != "moved.example.com" {
		t.Fatalf("the forward of the only host at its destination did not follow it: %q", got)
	}
	if msg := state.hostReferenceError([]uint32{updated.ID}); msg == "" {
		t.Fatal("deleting the only host a forward uses was allowed")
	}
}

func TestHostCatalogCarriesHostsThatShareADestination(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.hosts = state.hosts[:0]
	document := hostCatalogDocument{Format: hostCatalogFormat, Version: hostCatalogSchemaVersion, Hosts: bastionHosts()}
	if added, duplicates := state.applyHostCatalog(document); added != 2 || duplicates != 0 {
		t.Fatalf("added %d, duplicates %d", added, duplicates)
	}
	if added, duplicates := state.applyHostCatalog(document); added != 0 || duplicates != 2 {
		t.Fatalf("importing the catalog again added %d, duplicates %d", added, duplicates)
	}
	if len(state.hosts) != 2 || state.hosts[0].CatalogID == state.hosts[1].CatalogID {
		t.Fatalf("hosts = %+v", state.hosts)
	}
}

// The path a user takes: two targets behind one bastion are added by hand, a tab
// is opened on the second, and the window is closed and opened again.
func TestSecondHostAtADestinationIsAddedOpenedAndRestored(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	add := func(label string) {
		t.Helper()
		state.Click(hitAddHost)
		typeInto(state, hitDialogLabel, label)
		typeInto(state, hitDialogAddress, "bastion.example.com")
		typeInto(state, hitDialogUser, "ops")
		state.Click(hitDialogSubmit)
		if state.modal != modalNone || state.dialogError != "" {
			t.Fatalf("adding %q: modal=%d error=%q", label, state.modal, state.dialogError)
		}
	}
	add("Target A")
	add("Target B")
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "target b")
	typeInto(state, hitDialogAddress, "bastion.example.com")
	typeInto(state, hitDialogUser, "ops")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrAddressTaken {
		t.Fatalf("the same name at the same destination was accepted: modal=%d error=%q", state.modal, state.dialogError)
	}
	state.Click(hitDialogCancel)

	var second Host
	for _, host := range state.hosts {
		if host.Label == "Target B" {
			second = host
		}
	}
	session := state.openHostSession(second)
	if session == nil || session.host.CatalogID != second.CatalogID {
		t.Fatalf("session = %+v", session)
	}
	state.save()
	_ = world
	restarted := newTestState(t, options)
	for _, restored := range restarted.sessions {
		if restored.host.Label == "Target B" && restored.host.CatalogID == second.CatalogID {
			return
		}
	}
	t.Fatalf("the tab on the second host did not come back on it: %+v", restarted.sessions)
}
