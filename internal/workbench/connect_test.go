package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func typeSearch(state *State, value string) {
	app.TextEvent{Kind: app.TextInsert, Text: value}.Apply(state.EditorFor(hitSearch))
	state.syncVisible()
}

func connectHit(t *testing.T, state *State) ui.Hit {
	t.Helper()
	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, hitConnect)
	if !ok {
		t.Fatal("no Connect button")
	}
	return hit
}

func doubleClick(state *State, target ui.ID) ui.EventResult {
	return pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2, Target: target})
}

func TestConnectIsDisabledUntilTheSearchBoxHoldsSomething(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	if !connectHit(t, state).Disabled {
		t.Fatal("Connect is enabled with an empty search box")
	}
	state.Click(hitConnect)
	if len(world.conns) != 0 || len(state.sessions) != 0 {
		t.Fatal("a disabled Connect dialed")
	}

	typeSearch(state, "   ")
	if !connectHit(t, state).Disabled {
		t.Fatal("blanks alone enabled Connect")
	}
	typeSearch(state, "deploy@new.example.com")
	if connectHit(t, state).Disabled {
		t.Fatal("Connect is disabled although the box holds a target")
	}
	state.search = text.Editor{}
	if !connectHit(t, state).Disabled {
		t.Fatal("Connect stayed enabled after the box was emptied")
	}
}

func TestConnectOpensWhatTheSearchBoxHolds(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)

	// A target nobody saved: user, host and port are read from the text.
	typeSearch(state, "ops@10.0.0.9:2201")
	state.Click(hitConnect)
	if len(world.configs) != 1 {
		t.Fatalf("dials = %d, status %q", len(world.configs), state.status)
	}
	config := world.configs[0]
	if config.Host != "10.0.0.9" || config.Port != 2201 || config.User != "ops" {
		t.Fatalf("dial = %+v", config)
	}
	session := state.sessions[0]
	if session.HostID != 0 || session.Title != "10.0.0.9:2201" || session.Address != "ops@10.0.0.9:2201" {
		t.Fatalf("session = %+v", session)
	}
	if len(state.hosts) != 3 {
		t.Fatal("connecting to a typed target saved a host")
	}
	if state.activeSection != sectionTerminal {
		t.Fatal("connecting did not show the terminal")
	}

	// The same target again reuses its tab.
	state.Click(hitHosts)
	state.Click(hitConnect)
	if len(world.configs) != 1 || len(state.sessions) != 1 {
		t.Fatalf("a typed target already open was dialed again: dials=%d sessions=%d", len(world.configs), len(state.sessions))
	}
	// A different one gets its own.
	state.Click(hitHosts)
	state.search = text.Editor{}
	typeSearch(state, "other.example.com")
	state.Click(hitConnect)
	if len(world.configs) != 2 || len(state.sessions) != 2 || state.sessions[1].Address != state.login(Host{})+"@other.example.com:22" {
		t.Fatalf("second target: dials=%d sessions=%+v", len(world.configs), state.sessions)
	}
}

func TestConnectPrefersASavedHostNamedExactly(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)

	typeSearch(state, "staging")
	state.Click(hitConnect)
	if len(world.configs) != 1 || world.configs[0].Host != "staging.example.com" || world.configs[0].User != "deploy" {
		t.Fatalf("a saved label was not recognized: %+v (status %q)", world.configs, state.status)
	}
	if state.sessions[0].HostID != 2 || state.selectedHostID != 2 {
		t.Fatalf("session=%+v selected=%d", state.sessions[0], state.selectedHostID)
	}

	// The address works too, case-insensitively, and a partial name does not.
	state.Click(hitHosts)
	state.search = text.Editor{}
	typeSearch(state, "ROUTER")
	state.Click(hitConnect)
	if len(world.configs) != 2 || world.configs[1].Host != "192.168.1.1" {
		t.Fatalf("dials = %+v", world.configs)
	}
	state.Click(hitHosts)
	state.search = text.Editor{}
	typeSearch(state, "stag")
	state.Click(hitConnect)
	if len(world.configs) != 3 || world.configs[2].Host != "stag" {
		t.Fatalf("a partial name must be read as a host name, not matched to a saved host: %+v", world.configs)
	}
}

func TestConnectWithTextThatIsNotATargetSaysSoAndDialsNothing(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	typeSearch(state, "two words")
	state.Click(hitConnect)
	if len(world.configs) != 0 || len(state.sessions) != 0 {
		t.Fatal("unparseable text was dialed")
	}
	if want := `"two words" is not something to connect to. Write it as user@host or user@host:port.`; state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
	if state.activeSection != sectionHosts {
		t.Fatal("the window left the host list")
	}
}

func TestTypingNarrowsTheHostListAndTheCount(t *testing.T) {
	state := newTestState(t, testOptions(t))
	visibleLabels := func() []string {
		var labels []string
		for _, index := range state.visible {
			labels = append(labels, state.hosts[index].Label)
		}
		return labels
	}
	if got := visibleLabels(); len(got) != 3 {
		t.Fatalf("unfiltered: %v", got)
	}
	for query, want := range map[string][]string{
		"stag":     {"Staging"},
		"SERVERS":  {"Production", "Staging"}, // group, case-insensitive
		"admin":    {"Router"},                // user
		"192.168":  {"Router"},                // address
		"  prod  ": {"Production"},            // surrounding blanks do not matter
		"zzz":      nil,
		"":         {"Production", "Staging", "Router"},
	} {
		state.search = text.Editor{}
		typeSearch(state, query)
		got := visibleLabels()
		if len(got) != len(want) {
			t.Errorf("%q shows %v, want %v", query, got, want)
			continue
		}
		for index := range want {
			if got[index] != want[index] {
				t.Errorf("%q shows %v, want %v", query, got, want)
			}
		}
	}

	state.search = text.Editor{}
	typeSearch(state, "servers")

	// Painting shows exactly the matching rows, and none for no match.
	canvas, texts := paintedTexts(state)
	if !contains(texts, "Production") || !contains(texts, "Staging") || contains(texts, "Router") {
		t.Fatalf("painted rows: %v", texts)
	}
	for _, id := range []ui.ID{hitHostList.At(1), hitHostList.At(2)} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Errorf("row %d is not clickable", id)
		}
	}
	if _, ok := hitByID(canvas, hitHostList.At(3)); ok {
		t.Error("a hidden row is still clickable")
	}
	state.search = text.Editor{}
	typeSearch(state, "zzz")
	_, texts = paintedTexts(state)
	if !contains(texts, state.messages.NoMatches) || !contains(texts, state.messages.NoMatchesSub) {
		t.Fatalf("the no-match page is missing: %v", texts)
	}
}

func TestDoubleClickingARowConnectsToIt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)

	// A single click only selects.
	state.Click(hitHostList.At(2))
	if len(world.configs) != 0 || state.selectedHostID != 2 {
		t.Fatalf("single click: dials=%d selected=%d", len(world.configs), state.selectedHostID)
	}
	// Presses with another count, or on something else, are not double clicks.
	for _, event := range []ui.PointerEvent{
		{Kind: ui.PointerDown, ClickCount: 1, Target: hitHostList.At(2)},
		{Kind: ui.PointerDown, ClickCount: 3, Target: hitHostList.At(2)},
		{Kind: ui.PointerUp, ClickCount: 2, Target: hitHostList.At(2)},
		{Kind: ui.PointerMove, ClickCount: 0, Target: hitHostList.At(2)},
		{Kind: ui.PointerDown, ClickCount: 2, Target: hitAddHost},
		{Kind: ui.PointerDown, ClickCount: 2, Target: hitKeychain},
		{Kind: ui.PointerDown, ClickCount: 2, Target: hitTabList.At(1)},
		{Kind: ui.PointerDown, ClickCount: 2, Target: ui.ID{}},
	} {
		if result := pointer(state, event); result.Handled || len(world.configs) != 0 {
			t.Fatalf("%+v connected or was handled", event)
		}
	}

	// Two quick right clicks count as a double press, but only the primary
	// button connects.
	pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, ClickCount: 2, Target: hitHostList.At(2)})
	if len(world.configs) != 0 {
		t.Fatalf("a right double press connected: %+v", world.configs)
	}
	state.hostMenu.open = false

	result := doubleClick(state, hitHostList.At(3))
	if len(world.configs) != 1 || world.configs[0].Host != "192.168.1.1" || world.configs[0].User != "admin" {
		t.Fatalf("double click: %+v", world.configs)
	}
	if !result.Handled || !result.Repaint {
		t.Fatalf("result = %+v; the press must be handled so its release does not click the page that replaced the list", result)
	}
	if state.selectedHostID != 3 || state.activeSection != sectionTerminal {
		t.Fatalf("selected=%d section=%d", state.selectedHostID, state.activeSection)
	}

	// Not while a dialog is up, and not from the terminal page.
	state.Click(hitHosts)
	state.Click(hitAddHost)
	doubleClick(state, hitHostList.At(1))
	if len(world.configs) != 1 {
		t.Fatal("a double click behind a modal connected")
	}
}

func TestHostCatalogPointerDoesNotActivateAnOpenWorkspace(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitHostList.At(2))
	state.newHostSession(state.hosts[1])
	state.Click(hitHosts)

	pointer(state, ui.PointerEvent{Kind: ui.PointerMove, Target: hitHostGroupRowList.At(0)})

	if state.activeSection != sectionHosts {
		t.Fatalf("section = %d; moving over the host catalog activated an open workspace", state.activeSection)
	}
}

func TestEnterConnectsTheFocusedRowTheSelectedHostAndTheSearchBox(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	enter := func(target ui.ID) ui.EventResult {
		return state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: target})
	}

	// A focused row connects that row.
	if result := enter(hitHostList.At(2)); !result.Handled || len(world.configs) != 1 || world.configs[0].Host != "staging.example.com" {
		t.Fatalf("Enter on a row: %+v dials=%+v", result, world.configs)
	}
	state.Click(hitHosts)

	// Nothing focused: the selected host.
	state.selectedHostID = 3
	if result := enter(ui.ID{}); !result.Handled || len(world.configs) != 2 || world.configs[1].Host != "192.168.1.1" {
		t.Fatalf("Enter with the selected host: %+v dials=%+v", result, world.configs)
	}
	state.Click(hitHosts)

	// In the search box: what it holds.
	typeSearch(state, "tester@127.0.0.1:2222")
	if result := enter(hitSearch); !result.Handled || len(world.configs) != 3 || world.configs[2].Port != 2222 {
		t.Fatalf("Enter in the search box: %+v dials=%+v", result, world.configs)
	}
	state.Click(hitHosts)

	// Enter in an empty search box, or on a button, does not connect and is
	// left to the runtime (which activates a focused button).
	state.search = text.Editor{}
	for _, target := range []ui.ID{hitSearch, hitAddHost, hitKeychain} {
		if result := enter(target); result.Handled && target != hitSearch {
			t.Errorf("Enter on %d was taken", target)
		}
	}
	if len(world.configs) != 3 {
		t.Fatalf("dials = %d", len(world.configs))
	}

	// A held key repeats Enter; it must not open a second tab, and the
	// selected host hidden by the search box is not connected blindly.
	typeSearch(state, "router")
	state.selectedHostID = 1
	if result := enter(ui.ID{}); result.Handled || len(world.configs) != 3 {
		t.Fatalf("Enter connected a host the search box hides: %+v", result)
	}
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitHostList.At(1), Repeat: true}); result.Handled {
		t.Fatal("a repeated Enter was taken")
	}
	// Dialogs keep Enter for themselves.
	state.Click(hitAddHost)
	if result := enter(ui.ID{}); result.Handled || len(world.configs) != 3 {
		t.Fatal("Enter behind a dialog connected")
	}
}

func TestEscapeEmptiesTheSearchBox(t *testing.T) {
	state := newTestState(t, testOptions(t))
	typeSearch(state, "stag")
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitSearch}); !result.Handled || state.search.String() != "" {
		t.Fatalf("Escape: %+v text=%q", result, state.search.String())
	}
	if len(state.visible) != 3 {
		t.Fatal("clearing the box did not restore the list")
	}
	// With nothing to clear, Escape is left alone.
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape, Target: hitSearch}); result.Handled {
		t.Fatal("Escape on an empty box was swallowed")
	}
}

func TestOnlyTheHostsPageHasASearchEditor(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	if state.EditorFor(hitSearch) == nil {
		t.Fatal("the search box has no editor on the hosts page")
	}
	connect(t, state, world)
	if state.EditorFor(hitSearch) != nil {
		t.Fatal("the search box has an editor on the terminal page, where it is not painted")
	}
	state.Click(hitHosts)
	state.Click(hitAddHost)
	if state.EditorFor(hitSearch) != nil {
		t.Fatal("the search box has an editor under a dialog")
	}
}

func TestSearchFieldDeclaresTheGeometryOfItsOwnText(t *testing.T) {
	state := newTestState(t, testOptions(t))
	typeSearch(state, "prod")
	canvas, _ := paintedTexts(state)
	hit, _ := hitByID(canvas, hitSearch)
	var painted ui.Text
	for _, text := range canvas.Texts() {
		if text.Value == "prod" {
			painted = text
		}
	}
	if hit.TextSize != metrics.FontSizeM || painted.Value == "" && hit.TextBounds.Width == 0 {
		t.Fatalf("hit %+v painted %+v", hit, painted)
	}
	// The magnifier is drawn, and the native editor starts to its right rather
	// than over it.
	triangles := canvas.Triangles()
	var magnifierRight float32
	for _, triangle := range triangles {
		for _, point := range triangle.Points {
			inRow := point.Y >= hit.Bounds.Y && point.Y <= hit.Bounds.Y+hit.Bounds.Height
			if inRow && point.X < hit.Bounds.X {
				magnifierRight = max(magnifierRight, point.X)
			}
		}
	}
	if magnifierRight == 0 {
		t.Fatal("no magnifier was drawn in the search box")
	}
	if hit.TextBounds.X <= magnifierRight {
		t.Fatalf("native text starts at %v, magnifier reaches %v: they overlap", hit.TextBounds.X, magnifierRight)
	}
}

func TestSearchBoxKeepsSteadyStatePaintAllocationFree(t *testing.T) {
	state := newTestState(t, testOptions(t))
	typeSearch(state, "servers")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("paint with a filter allocated %v times", allocations)
	}
}

func TestConnectingAndFailingStillWorksFromATypedTarget(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	typeSearch(state, "solo.example.com")
	state.Click(hitConnect)
	fake := world.conns[0]
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = sshconn.ErrAuthFailed })
	state.Wake()
	if state.sessions[0].tone != toneFail {
		t.Fatal("a typed target's failure was not shown")
	}
	// Opening it again reconnects the same tab.
	state.Click(hitHosts)
	state.Click(hitConnect)
	if len(world.conns) != 2 || len(state.sessions) != 1 {
		t.Fatalf("conns=%d sessions=%d", len(world.conns), len(state.sessions))
	}
}

func TestTypingASavedHostInFullOpensThatHostNotADuplicate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	openHost(state) // Production, deploy@prod.example.com:22
	state.Click(hitHosts)

	for _, typed := range []string{"deploy@prod.example.com", "prod.example.com:22", "ssh deploy@prod.example.com -p 22"} {
		state.search = text.Editor{}
		typeSearch(state, typed)
		state.Click(hitConnect)
		if len(world.configs) != 1 || len(state.sessions) != 1 {
			t.Fatalf("%q opened a duplicate: dials=%d sessions=%d", typed, len(world.configs), len(state.sessions))
		}
		state.Click(hitHosts)
	}

	// A different user or port on the same machine is a different target.
	for _, step := range []struct {
		typed    string
		sessions int
	}{{"other@prod.example.com", 2}, {"deploy@prod.example.com:2222", 3}} {
		state.search = text.Editor{}
		typeSearch(state, step.typed)
		state.Click(hitConnect)
		if len(state.sessions) != step.sessions {
			t.Fatalf("%q: sessions = %d, want %d", step.typed, len(state.sessions), step.sessions)
		}
		state.Click(hitHosts)
	}
}
