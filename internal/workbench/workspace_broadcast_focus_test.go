package workbench

import (
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"testing"
)

func TestWorkspaceBroadcastClickKeepsTypingInTerminal(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1)
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	view, err := headless.Open(app.Config{Title: "broadcast focus", Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
		TextInput: state.TerminalInput}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	terminal := state.paneIdentity(session, hitTerminalList.At(int(session.ID)))
	hit, ok := hitByIDSnapshot(view.Snapshot().Hits, terminal)
	if !ok {
		t.Fatal("terminal missing")
	}
	if err := view.Click(hit.Bounds.X+20, hit.Bounds.Y+20); err != nil {
		t.Fatal(err)
	}
	t.Logf("before broadcast click: focus=%v terminal=%v", view.Snapshot().Focused, terminal)
	hit, ok = hitByIDSnapshot(view.Snapshot().Hits, hitWorkspaceHeaderBroadcast)
	if !ok || hit.Disabled {
		t.Fatalf("broadcast unavailable: %+v", hit)
	}
	if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	t.Logf("after broadcast click: focus=%v terminal=%v broadcast=%v", view.Snapshot().Focused, terminal, state.broadcast)
	if err := view.Type("probe"); err != nil {
		t.Logf("typing returned: %v", err)
	}
	for index, conn := range world.conns {
		t.Logf("fake shell %d input=%q", index, conn.shell(t).typed())
	}
	if view.Snapshot().Focused != terminal {
		t.Fatal("broadcast control took terminal input focus")
	}
	for _, conn := range world.conns {
		if got := conn.shell(t).typed(); got != "probe" {
			t.Fatalf("broadcast typed %q, want probe", got)
		}
	}
}
