package workbench

import (
	"encoding/json"
	"slices"
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

func TestNewHostsGetIDsThatSayNothingAboutThem(t *testing.T) {
	used := map[string]struct{}{}
	seen := map[string]bool{}
	for range 200 {
		id := freshCatalogID(used)
		if seen[id] || !validCatalogID(id) {
			t.Fatalf("catalog ID %q repeated or not accepted", id)
		}
		seen[id] = true
	}
	// A file from before IDs existed still gets the ID its host always had.
	legacy := Host{Address: "bastion.example.com", User: "ops", Port: 22}
	if got := uniqueCatalogID("", legacy, map[string]struct{}{}); got != catalogIDFor(legacy) {
		t.Fatalf("an ID-less host was given %q, want the one derived from its address", got)
	}
}

func TestMergeSameHostsKeepsTheSmallestIDWhateverTheOrder(t *testing.T) {
	host := func(id, label, address string) Host {
		return Host{ID: uint32(len(id)), CatalogID: id, Label: label, Address: address, User: "ops", Port: 22}
	}
	hosts := []Host{
		host("host_c", "Target A", "bastion.example.com"),
		host("host_x", "Other", "bastion.example.com"), // another name: its own host
		host("host_a", "target a", "bastion.example.com"),
		host("host_b", "Target A", "bastion.example.com"),
		host("host_c", "Target A", "bastion.example.com"), // the same record twice is not two hosts
	}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}} {
		var input []Host
		for _, index := range order {
			input = append(input, hosts[index])
		}
		kept, merges := mergeSameHosts(input)
		var ids []string
		for _, host := range kept {
			ids = append(ids, host.CatalogID)
		}
		slices.Sort(ids)
		if !slices.Equal(ids, []string{"host_a", "host_x"}) {
			t.Fatalf("order %v kept %v", order, ids)
		}
		aliases := mergeAliases(merges)
		if aliases["host_b"] != "host_a" || aliases["host_c"] != "host_a" || len(aliases) != 2 {
			t.Fatalf("order %v aliases = %v (a host folded twice must end on the one that stays)", order, aliases)
		}
	}
	// A serial host is its device: the account and port that mean nothing to it
	// do not keep two of them apart.
	serial := Host{CatalogID: "host_s1", Label: "Console", Address: "COM7", Protocol: protocolSerial}
	other := serial
	other.CatalogID, other.User, other.Port = "host_s2", "x", 9
	if kept, _ := mergeSameHosts([]Host{serial, other}); len(kept) != 1 {
		t.Fatalf("serial hosts not merged: %+v", kept)
	}
	// Hosts that differ in what they connect to are never folded.
	same, other2 := hosts[0], hosts[0]
	other2.CatalogID, other2.Port = "host_other", 2222
	if kept, merges := mergeSameHosts([]Host{same, other2}); len(kept) != 2 || len(merges) != 0 {
		t.Fatalf("different ports merged: %+v", kept)
	}
}

// Two devices that each add the same host have two IDs for it once their records
// meet. Reading them folds the two into one, with the references of the other
// moved over, and records that so the store holds one host.
func TestTheSameHostAddedOnTwoDevicesBecomesOneAndKeepsWhatReferredToIt(t *testing.T) {
	options := testOptions(t)
	// The password was typed for the host as the other device knew it.
	if err := newTestState(t, options).secrets.vault.Set(hostSecretName("host_b", hostPasswordPart), "typed on the other device"); err != nil {
		t.Fatal(err)
	}
	hosts := []savedHost{
		{ID: "host_b", Label: "Target A", Address: "bastion.example.com", User: "ops", Port: 22},
		{ID: "host_a", Label: "Target A", Address: "bastion.example.com", User: "ops", Port: 22},
		{ID: "host_jump", Label: "Jump", Address: "jump.example.com", User: "ops", Port: 22},
	}
	err := saveState(options.StatePath, savedState{
		Hosts:         hosts,
		AgentHosts:    []string{"host_b"},
		Sessions:      []savedSession{{Label: "Target A", Address: "bastion.example.com", User: "ops", Port: 22, SavedHost: true, CatalogID: "host_b"}},
		ActiveSession: 1,
		Proxies: []savedProxy{{
			TargetID: "host_b", JumpID: "host_jump", TargetAddress: "bastion.example.com", TargetUser: "ops", TargetPort: 22,
			JumpAddress: "jump.example.com", JumpUser: "ops", JumpPort: 22,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	var labels []string
	for _, host := range state.hosts {
		labels = append(labels, host.CatalogID)
	}
	if !slices.Equal(labels, []string{"host_a", "host_jump"}) {
		t.Fatalf("hosts after the fold = %v", labels)
	}
	if len(state.sessions) != 1 || state.sessions[0].host.CatalogID != "host_a" {
		t.Fatalf("the open tab was not moved to the host that stayed: %+v", state.sessions)
	}
	if !state.agentHosts["host_a"] || state.agentHosts["host_b"] {
		t.Fatalf("agent access did not follow the host: %v", state.agentHosts)
	}
	if len(state.proxies) != 1 || state.hosts[0].ID != state.proxies[0].TargetID {
		t.Fatalf("the jump route did not follow the host: %+v", state.proxies)
	}
	if value, found := vaultValue(t, state, hostSecretName("host_a", hostPasswordPart)); !found || value != "typed on the other device" {
		t.Fatalf("the saved password did not follow the host: %q %v", value, found)
	}
	restarted := newTestState(t, options)
	if len(restarted.hosts) != 2 || len(restarted.sessions) != 1 || restarted.sessions[0].host.CatalogID != "host_a" {
		t.Fatalf("the fold was not recorded: hosts=%d sessions=%d", len(restarted.hosts), len(restarted.sessions))
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
		if host.Label == "Target A" || host.Label == "Target B" {
			if host.CatalogID == catalogIDFor(host) {
				t.Fatalf("%s was given the ID its address would give", host.Label)
			}
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
