package workbench

import (
	"reflect"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

// terminalRailSpec is the catalog row of one terminal rail tool.
func terminalRailSpec(t *testing.T, id gfx.ID) toolbarSpec {
	t.Helper()
	for _, spec := range toolbarCatalog[toolbarTerminal] {
		if spec.id == id {
			return spec
		}
	}
	t.Fatalf("the terminal rail catalog has no row for %v", id)
	return toolbarSpec{}
}

// sidebarPageIcon is the glyph kind the sidebar row of one page wears.
func sidebarPageIcon(t *testing.T, page navPage) uint8 {
	t.Helper()
	for _, entry := range sidebarPages {
		if entry.page == page {
			return entry.icon
		}
	}
	t.Fatalf("the sidebar has no row for page %v", page)
	return 0
}

func TestTheRailRowsThatWereHardToReadHaveTheirOwnGlyphs(t *testing.T) {
	snippets := terminalRailSpec(t, hitTerminalSnippets)
	if !reflect.DeepEqual(lucideIcons[snippets.icon], icons.Zap) {
		t.Errorf("the %s row does not draw the quick-command bolt", snippets.key)
	}
	if snippets.icon == iconNotebook {
		t.Errorf("the %s row still borrows the notes glyph", snippets.key)
	}

	proxy := terminalRailSpec(t, hitLocalProxyOpen)
	if !reflect.DeepEqual(lucideIcons[proxy.icon], icons.Waypoints) {
		t.Errorf("the %s row does not draw the waypoints glyph", proxy.key)
	}
	if proxy.icon == iconGlobe {
		t.Errorf("the %s row still draws the globe the SSH browser draws", proxy.key)
	}
	if actionIcon(actionBrowser) != iconGlobe {
		t.Error("the SSH browser lost the globe its menu rows name it with")
	}
}

// The row a connected terminal actually offers is the one the user reads, so the
// live tools list carries the same glyphs the catalog declares.
func TestTheOfferedTerminalRowsWearThoseGlyphs(t *testing.T) {
	world, options := newSharingWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()

	snippets, ok := offeredTerminalTool(state, hitTerminalSnippets)
	if !ok {
		t.Fatal("a connected terminal offers no quick-command row")
	}
	if snippets.icon != iconSnippets {
		t.Errorf("the offered quick-command row wears %v, want %v", snippets.icon, iconSnippets)
	}
	proxy, ok := offeredTerminalTool(state, hitLocalProxyOpen)
	if !ok {
		t.Fatal("a connected SSH terminal offers no local-proxy row")
	}
	if proxy.icon != iconLocalProxy {
		t.Errorf("the offered local-proxy row wears %v, want %v", proxy.icon, iconLocalProxy)
	}
}

// Hiding the rail row does not hide the tunnel: while a proxy runs, the entry
// reappears in the toolbar dock, and the stand-in has to look like the row it
// replaces.
func TestTheDockedLocalProxyRowWearsTheSameGlyphAsTheRailRow(t *testing.T) {
	world, options := newSharingWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Wake()
	state.sessions[0].term.shell.(*fakeShell).say("ready")
	state.Wake()
	// The machine's own proxy would prefill the panel port. That is a property of
	// the laptop running the test, not of this row.
	state.lookupSystemProxy = nil
	state.Click(hitLocalProxyOpen)
	if state.modal != modalLocalProxy {
		t.Fatal("the rail row did not open the local proxy panel")
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	state.toolbars.customize(toolbarTerminal)
	for index := range state.toolbars.layouts[toolbarTerminal] {
		if state.toolbars.layouts[toolbarTerminal][index].Key == "local-proxy" {
			state.toolbars.layouts[toolbarTerminal][index].Enabled = false
		}
	}
	state.localProxyUI.port.Insert("43217")
	state.activeSection = sectionTerminal
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("a connected session is missing")
	}
	state.localProxyUI.session = session.ID
	target, ok := state.localProxyTarget(session)
	if !ok {
		t.Fatal("a connected SSH session has no local proxy target")
	}
	state.localProxyManager().operations[target] = &localProxyOperation{
		target: target, cancel: func() {}, phase: localProxyConnected,
		users: make(map[localProxySubscription]struct{}),
	}
	var tools [maxBarTools]barTool
	for _, tool := range state.toolbarDockTools(state.defaultToolbarDockZone(toolbarTerminal), &tools) {
		if tool.id != hitLocalProxyOpen {
			continue
		}
		if tool.icon != iconLocalProxy {
			t.Errorf("the docked local-proxy row wears %v, want %v", tool.icon, iconLocalProxy)
		}
		return
	}
	t.Fatalf("hiding the rail row left no docked local-proxy entry: progress=%v", state.showsProgress(session))
}

func TestSnippetSidebarAndToolbarUseTheSameGlyph(t *testing.T) {
	if got, want := sidebarPageIcon(t, navPageSnippets), terminalRailSpec(t, hitTerminalSnippets).icon; got != want {
		t.Errorf("the snippets sidebar row wears %v and the rail row wears %v", got, want)
	}
	if got := sidebarPageIcon(t, navPageSnippets); !reflect.DeepEqual(lucideIcons[got], icons.Zap) {
		t.Error("the snippets sidebar row does not draw the quick-command bolt")
	}
}

// A row that animates when it is pressed is a row the press reached. Both glyphs
// join the shapes that settle with a breath rather than a turn or a swing.
func TestTheNewRailGlyphsPlayTheMotionOfTheirKind(t *testing.T) {
	for _, c := range []struct {
		kind  uint8
		glyph gfx.Icon
	}{
		{iconSnippets, icons.Zap},
		{iconLocalProxy, icons.Waypoints},
	} {
		motion, ok := iconMotionFor(c.kind)
		if !ok || !sameMotion(motion, iconMotionSquash) {
			t.Errorf("icon kind %v plays %v with %d parts (found %v), want the squash motion", c.kind, motion.duration, len(motion.parts), ok)
		}
		if !reflect.DeepEqual(lucideIcons[c.kind], c.glyph) {
			t.Errorf("icon kind %v does not draw the glyph its row is named for", c.kind)
		}
	}
}
