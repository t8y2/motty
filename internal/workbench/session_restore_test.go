package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestSessionsRestoreWithoutDialingAndReconnectOnEnter(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	first.Click(hitHosts)
	first.Click(hitHostList.At(2))
	openHost(first)
	first.Click(hitTabList.At(int(first.sessions[0].ID)))
	activeAddress := first.sessions[0].Address
	if len(world.conns) != 2 {
		t.Fatalf("initial dials = %d", len(world.conns))
	}

	second := newTestState(t, options)
	if len(world.conns) != 2 {
		t.Fatalf("restoring dialed %d extra connections", len(world.conns)-2)
	}
	if len(second.sessions) != 2 || second.activeSection != sectionTerminal {
		t.Fatalf("restored sessions=%d section=%d", len(second.sessions), second.activeSection)
	}
	active, ok := second.sessionByIDPointer(second.activeSessionID)
	if !ok || active.Address != activeAddress || active.conn != nil || active.phase != sshconn.PhaseClosed {
		t.Fatalf("restored active session = %+v", active)
	}
	if active.term != nil || !active.restored || !second.canReconnect(active) {
		t.Fatalf("restored session = %+v", active)
	}
	canvas, _ := paintedTexts(second)
	retry, close, log := hitConnRetry, hitConnClose, hitConnLog
	if !hasHit(canvas, retry) || !hasHit(canvas, close) || hasHit(canvas, log) {
		t.Fatalf("restored page hits = %+v", canvas.Hits())
	}
	var actionTop float32
	for _, hit := range canvas.Hits() {
		if hit.ID == close || hit.ID == retry {
			if actionTop == 0 || hit.Bounds.Y < actionTop {
				actionTop = hit.Bounds.Y
			}
		}
	}
	// The explanation may wrap onto several lines; it reads as the title,
	// then the body, all above the buttons and in order.
	var title ui.Text
	var body strings.Builder
	var bodyBottom float32
	for _, text := range canvas.Texts() {
		switch {
		case text.Value == second.messages.SessionRestoredTitle:
			title = text
		case title.Value != "" && text.Bounds.Y > title.Bounds.Y && text.Bounds.Y < actionTop && text.Bounds.Y > windowBarHeight:
			body.WriteString(text.Value)
			bodyBottom = max(bodyBottom, text.Bounds.Y+text.Bounds.Height)
		}
	}
	if title.Value == "" {
		t.Fatalf("restored page has no %q", second.messages.SessionRestoredTitle)
	}
	if strings.ReplaceAll(body.String(), " ", "") != strings.ReplaceAll(second.messages.SessionRestoredBody, " ", "") {
		t.Fatalf("restored body = %q, want %q", body.String(), second.messages.SessionRestoredBody)
	}
	if actionTop == 0 || bodyBottom > actionTop {
		t.Fatalf("restored body bottom=%v action top=%v", bodyBottom, actionTop)
	}
	// The divider above the buttons must not run through the explanation.
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Height == 1 && rectangle.Bounds.Y > title.Bounds.Y && rectangle.Bounds.Y < bodyBottom && rectangle.Bounds.Width > 100 {
			t.Fatalf("a rule at y=%v crosses the explanation (title y=%v, bottom=%v)", rectangle.Bounds.Y, title.Bounds.Y, bodyBottom)
		}
	}

	before := len(world.conns)
	second.Click(retry)
	if len(world.conns) != before+1 {
		t.Fatalf("reconnect dials=%d", len(world.conns)-before)
	}
	if len(second.sessions) != 2 || second.activeSessionID != active.ID || active.conn == nil || active.phase != sshconn.PhaseConnecting || active.restored {
		t.Fatalf("reconnected session = %+v", active)
	}
}

func TestWorkspaceAndClosedTabsPersist(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	first.Click(hitHosts)
	first.Click(hitHostList.At(2))
	openHost(first)
	closed := first.sessions[0].Address
	kept := first.sessions[1].Address
	first.Click(hitTabCloseList.At(int(first.sessions[0].ID)))
	first.Click(hitKeychain)

	second := newTestState(t, options)
	if second.activeSection != sectionKeys {
		t.Fatalf("restored section = %d", second.activeSection)
	}
	if len(second.sessions) != 1 || second.sessions[0].Address != kept || second.sessions[0].Address == closed {
		t.Fatalf("restored sessions = %+v", second.sessions)
	}
	second.closeWorkspace(second.deck.ActiveWorkspace())
	third := newTestState(t, options)
	if len(third.sessions) != 0 || third.activeSection != sectionHosts {
		t.Fatalf("closed restored tab returned: sessions=%+v section=%d", third.sessions, third.activeSection)
	}
}

func TestTypedSessionRestoresAsAnEphemeralTarget(t *testing.T) {
	world, options := newWorld(t)
	first := newTestState(t, options)
	first.search.Insert("guest@typed.example.com:2222")
	first.Click(hitConnect)
	if len(world.conns) != 1 || len(first.sessions) != 1 || first.sessions[0].HostID != 0 {
		t.Fatalf("typed session = %+v dials=%d", first.sessions, len(world.conns))
	}

	second := newTestState(t, options)
	if len(world.conns) != 1 {
		t.Fatalf("restoring typed target dialed again: %d", len(world.conns))
	}
	if len(second.sessions) != 1 {
		t.Fatalf("restored typed sessions = %+v", second.sessions)
	}
	session := second.sessions[0]
	if session.HostID != 0 || session.host.User != "guest" || session.host.Address != "typed.example.com" || session.host.Port != 2222 {
		t.Fatalf("restored typed target = %+v", session)
	}
}

func TestSameEndpointProfilesRestoreTheirOwnSessionsRoutesAndForwards(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	clickHostAction(state, hitCopyHost)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || len(state.hosts) != 4 {
		t.Fatalf("copy host: modal=%d error=%q hosts=%d", state.modal, state.dialogError, len(state.hosts))
	}
	original, duplicate := state.hosts[0], state.hosts[3]

	state.selectedHostID = original.ID
	openHost(state)
	state.Click(hitHosts)
	state.selectedHostID = duplicate.ID
	openHost(state)
	remotePort := freePort(t)
	state.selectedHostID = original.ID
	addRemoteForwardRule(t, state, "Original", remotePort, "127.0.0.1", 8080)
	state.selectedHostID = duplicate.ID
	addRemoteForwardRule(t, state, "Duplicate", remotePort, "127.0.0.1", 8080)
	addProxyRoute(t, state, duplicate.ID, original.ID)

	snapshot := state.snapshot()
	if len(snapshot.Sessions) != 2 || snapshot.Sessions[0].HostCatalogID != original.CatalogID || snapshot.Sessions[1].HostCatalogID != duplicate.CatalogID {
		t.Fatalf("saved sessions = %+v", snapshot.Sessions)
	}
	if len(snapshot.Forwards) != 2 || snapshot.Forwards[0].HostCatalogID != original.CatalogID || snapshot.Forwards[1].HostCatalogID != duplicate.CatalogID {
		t.Fatalf("saved forwards = %+v", snapshot.Forwards)
	}
	if len(snapshot.Proxies) != 1 || snapshot.Proxies[0].TargetCatalogID != duplicate.CatalogID || snapshot.Proxies[0].JumpCatalogID != original.CatalogID {
		t.Fatalf("saved routes = %+v", snapshot.Proxies)
	}

	restarted := newTestState(t, options)
	if len(restarted.sessions) != 2 {
		t.Fatalf("restored sessions = %+v", restarted.sessions)
	}
	for index, want := range []Host{original, duplicate} {
		session := restarted.sessions[index]
		if session.host.CatalogID != want.CatalogID || session.HostID != session.host.ID {
			t.Errorf("session %d restored to %+v, want %s", index, session.host, want.Label)
		}
	}
	if len(restarted.forwards) != 2 {
		t.Fatalf("restored forwards = %+v", restarted.forwards)
	}
	for index, want := range []Host{original, duplicate} {
		forward := restarted.forwards[index]
		host, ok := restarted.forwardHost(forward)
		if !ok || host.CatalogID != want.CatalogID || forward.HostCatalogID != want.CatalogID {
			t.Errorf("forward %d restored to %+v (%+v), want %s", index, host, forward, want.Label)
		}
	}
	if len(restarted.proxies) != 1 {
		t.Fatalf("restored routes = %+v", restarted.proxies)
	}
	target, targetOK := restarted.hostByID(restarted.proxies[0].TargetID)
	jump, jumpOK := restarted.hostByID(restarted.proxies[0].JumpID)
	if !targetOK || !jumpOK || target.CatalogID != duplicate.CatalogID || jump.CatalogID != original.CatalogID {
		t.Fatalf("restored route target=%+v jump=%+v", target, jump)
	}
}

func TestLegacyReferencesMigrateToTheFirstMatchingEndpoint(t *testing.T) {
	options := freshOptions(t).withDefaults()
	hosts := []savedHost{
		{ID: "first", Label: "First", Address: "same.example.com", User: "ops", Port: 22},
		{ID: "second", Label: "Second", Address: "same.example.com", User: "ops", Port: 22},
		{ID: "jump", Label: "Jump", Address: "jump.example.com", User: "ops", Port: 22},
	}
	if err := saveState(options.StatePath, savedState{
		Hosts: hosts,
		Sessions: []savedSession{{
			Label: "Legacy", Address: "same.example.com", User: "ops", Port: 22, SavedHost: true,
		}},
		Forwards: []savedForward{{
			Label: "Legacy", HostAddress: "same.example.com", HostUser: "ops", HostPort: 22,
			LocalPort: 9000, DestinationHost: "localhost", DestinationPort: 80,
		}},
		Proxies: []savedProxy{{
			TargetAddress: "same.example.com", TargetUser: "ops", TargetPort: 22,
			JumpAddress: "jump.example.com", JumpUser: "ops", JumpPort: 22,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	first := state.hosts[0]
	if len(state.sessions) != 1 || state.sessions[0].host.CatalogID != first.CatalogID {
		t.Fatalf("legacy sessions = %+v", state.sessions)
	}
	if len(state.forwards) != 1 || state.forwards[0].HostCatalogID != first.CatalogID {
		t.Fatalf("legacy forwards = %+v", state.forwards)
	}
	if len(state.proxies) != 1 || state.proxies[0].TargetID != first.ID || state.proxies[0].JumpID != state.hosts[2].ID {
		t.Fatalf("legacy routes = %+v", state.proxies)
	}
	snapshot := state.snapshot()
	if snapshot.Sessions[0].HostCatalogID != first.CatalogID || snapshot.Forwards[0].HostCatalogID != first.CatalogID ||
		snapshot.Proxies[0].TargetCatalogID != first.CatalogID || snapshot.Proxies[0].JumpCatalogID != state.hosts[2].CatalogID {
		t.Fatalf("migration was not saved: sessions=%+v forwards=%+v routes=%+v", snapshot.Sessions, snapshot.Forwards, snapshot.Proxies)
	}
}

func TestUnknownCatalogIdentitiesDoNotFallBackToAnEndpoint(t *testing.T) {
	options := freshOptions(t).withDefaults()
	hosts := []savedHost{
		{ID: "first", Label: "First", Address: "same.example.com", User: "ops", Port: 22},
		{ID: "second", Label: "Second", Address: "same.example.com", User: "ops", Port: 22},
	}
	if err := saveState(options.StatePath, savedState{
		Hosts: hosts,
		Sessions: []savedSession{{
			Label: "Unknown", HostCatalogID: "missing", Address: "same.example.com", User: "ops", Port: 22, SavedHost: true,
		}},
		Forwards: []savedForward{{
			Label: "Unknown", HostCatalogID: "missing", HostAddress: "same.example.com", HostUser: "ops", HostPort: 22,
			LocalPort: 9000, DestinationHost: "localhost", DestinationPort: 80,
		}},
		Proxies: []savedProxy{{
			TargetCatalogID: "missing", TargetAddress: "same.example.com", TargetUser: "ops", TargetPort: 22,
			JumpCatalogID: "second", JumpAddress: "same.example.com", JumpUser: "ops", JumpPort: 22,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.sessions) != 0 || len(state.proxies) != 0 {
		t.Fatalf("unknown identities rebound: sessions=%+v routes=%+v", state.sessions, state.proxies)
	}
	if len(state.forwards) != 1 {
		t.Fatalf("unknown forward was dropped: %+v", state.forwards)
	}
	if _, ok := state.forwardHost(state.forwards[0]); ok || state.forwardHostAvailable(state.forwards[0]) {
		t.Fatalf("unknown forward rebound to an endpoint: %+v", state.forwards[0])
	}
}

func TestRestoreSessionsSkipsInvalidStaleDuplicateAndExcessEntries(t *testing.T) {
	options := freshOptions(t)
	entries := []savedSession{
		{Address: ""},
		{Label: "Stale", Address: "stale.example.com", Port: 22, SavedHost: true},
		{Label: "One", Address: "one.example.com", Port: 22},
		{Label: "Duplicate", Address: "one.example.com", Port: 22},
	}
	for index := 0; index < maxRestoredSessions+4; index++ {
		entries = append(entries, savedSession{Label: fmt.Sprintf("Host %d", index), Address: fmt.Sprintf("host-%d.example.com", index), Port: 22})
	}
	if err := saveState(options.withDefaults().StatePath, savedState{Sessions: entries, Workspace: "terminal", ActiveSession: 1}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.sessions) != maxRestoredSessions {
		t.Fatalf("restored session count = %d", len(state.sessions))
	}
	if state.sessions[0].Address != state.systemUser+"@one.example.com:22" {
		t.Fatalf("first restored session = %+v", state.sessions[0])
	}
	if state.activeSessionID != state.sessions[0].ID || state.activeSection != sectionTerminal {
		t.Fatalf("active=%d section=%d", state.activeSessionID, state.activeSection)
	}
}

func TestRestoredSessionIdlePathsDoNotAllocate(t *testing.T) {
	options := freshOptions(t)
	options = options.withDefaults()
	if err := saveState(options.StatePath, savedState{
		Workspace: "terminal",
		Sessions:  []savedSession{{Label: "Restored", Address: "restored.example.com", User: "user", Port: 22}},
	}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if allocations := testing.AllocsPerRun(100, state.Wake); allocations != 0 {
		t.Fatalf("restored Wake allocations = %v", allocations)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}); allocations != 0 {
		t.Fatalf("restored paint allocations = %v", allocations)
	}
}

func BenchmarkRestoreWorkspace(b *testing.B) {
	for _, count := range []int{0, 8} {
		b.Run(fmt.Sprintf("sessions_%d", count), func(b *testing.B) {
			options := freshOptions(b).withDefaults()
			saved := savedState{Workspace: "terminal", Sessions: make([]savedSession, count)}
			for index := range saved.Sessions {
				saved.Sessions[index] = savedSession{
					Label: fmt.Sprintf("Host %d", index), Address: fmt.Sprintf("host-%d.example.com", index),
					User: "deploy", Port: 22,
				}
			}
			if err := saveState(options.StatePath, saved); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = newTestState(b, options)
			}
		})
	}
}
