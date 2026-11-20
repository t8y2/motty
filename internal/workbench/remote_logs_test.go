//go:build darwin || windows || linux

package workbench

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestRemoteLogsWithCommandsBar(t *testing.T) {
	for _, width := range []int{800, 1444} {
		for _, logsFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/logsFirst=%v", width, logsFirst), func(t *testing.T) {
				state, _, _, _ := liveSession(t)
				state.snippets.list = []snippet{{Name: "pwd", Command: "pwd"}, {Name: "ls", Command: "ls"}}
				view, err := headless.Open(state.WindowConfig(app.Config{
					Width: width, Height: 808, Tree: Tree(),
					Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
					OnScroll: state.Scroll, OnResize: state.Resize, OnWake: state.Wake,
				}), nil, headless.Options{Scale: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				click := func(id gfx.ID) {
					t.Helper()
					for _, hit := range view.Snapshot().Hits {
						if hit.ID == id {
							x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
							if err := view.Click(x, y); err != nil {
								t.Fatal(err)
							}
							if err := view.Rest(); err != nil {
								t.Fatal(err)
							}
							return
						}
					}
					t.Fatalf("missing control %v", id)
				}
				if logsFirst {
					click(hitServerLogs)
				}
				click(hitTerminalSnippets)
				click(hitCommandsBarToggle)
				if err := view.Keys("Escape"); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				if !logsFirst {
					click(hitServerLogs)
				}
				assertSeparate := func() {
					t.Helper()
					session, _ := state.sessionByIDPointer(state.activeSessionID)
					if session.remoteLogs == nil || !state.commands.shown {
						t.Fatal("both panels must be open")
					}
					bar := state.commands.bar
					paths, err := view.Locate(state.messages.RemoteLogs.ResizePaths)
					if err != nil {
						t.Fatal(err)
					}
					if bar.Height <= 0 || paths.Bounds.Y+paths.Bounds.Height > bar.Y {
						t.Fatalf("server logs overlap commands bar: logs splitter=%+v bar=%+v", paths.Bounds, bar)
					}
				}
				if dir := os.Getenv("MOTTY_REMOTE_LOG_SCREENSHOTS"); dir != "" {
					if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("logs-commands-%d-%v.png", width, logsFirst))); err != nil {
						t.Fatal(err)
					}
				}
				assertSeparate()
				for _, label := range []string{state.messages.RemoteLogs.Expand, state.messages.RemoteLogs.Restore} {
					if err := view.ClickLabel(label); err != nil {
						t.Fatal(err)
					}
					if err := view.Rest(); err != nil {
						t.Fatal(err)
					}
					assertSeparate()
				}
				dividerFound := false
				for _, hit := range view.Snapshot().Hits {
					if hit.ID != hitRemoteLogDivider {
						continue
					}
					dividerFound = true
					x, y := hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2
					if err := view.Press(x, y); err != nil {
						t.Fatal(err)
					}
					if err := view.Move(x, y-60); err != nil {
						t.Fatal(err)
					}
					if err := view.Release(x, y-60); err != nil {
						t.Fatal(err)
					}
					session, _ := state.sessionByIDPointer(state.activeSessionID)
					if session.remoteLogs.height <= 360 {
						t.Fatal("dragging the log divider did not resize the panel")
					}
				}
				if !dividerFound {
					t.Fatal("missing log panel divider")
				}
				assertSeparate()
				if err := view.Resize(width, 600); err != nil {
					t.Fatal(err)
				}
				assertSeparate()
				click(hitCommandsClose)
				if state.commands.shown {
					t.Fatal("commands close control did not hide the bar")
				}
				click(hitServerLogs)
				session, _ := state.sessionByIDPointer(state.activeSessionID)
				if session.remoteLogs != nil {
					t.Fatal("server logs toggle did not close the panel")
				}
			})
		}
	}
}

func TestRemoteLogsDockDragReorderAndPersist(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Server logs", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
		OnResize: state.Resize, OnWake: state.Wake,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	toolbarDockShot(t, view, "server-logs-default")

	logs := dualHit(t, view, hitServerLogs)
	notes := dualHit(t, view, hitNotesPanel)
	if err := view.Press(logs.X+logs.Width/2, logs.Y+logs.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(notes.X+notes.Width/2, notes.Y+2); err != nil {
		t.Fatal(err)
	}
	if !state.toolbarDock.drag.active {
		t.Fatal("server logs did not start a toolbar drag")
	}
	if err := view.Release(notes.X+notes.Width/2, notes.Y+2); err != nil {
		t.Fatal(err)
	}
	if zone := state.toolbarDockZoneFor(toolbarTerminal, "remote-logs"); zone != toolbarDockSideTop {
		t.Fatalf("server logs zone = %q, want %q", zone, toolbarDockSideTop)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if logs, notes = dualHit(t, view, hitServerLogs), dualHit(t, view, hitNotesPanel); logs.Y >= notes.Y {
		t.Fatalf("server logs were not reordered before notes: logs=%v notes=%v", logs, notes)
	}
	toolbarDockShot(t, view, "server-logs-reordered")
	want := state.toolbars.snapshot()
	reopened := newTestState(t, state.options)
	if !reflect.DeepEqual(reopened.toolbars.snapshot(), want) {
		t.Fatal("server logs placement did not survive restart")
	}
	if err := view.ClickLabel(state.messages.RemoteLogs.Title); err != nil {
		t.Fatal(err)
	}
	if session.remoteLogs == nil {
		t.Fatal("reordered server logs button did not open the panel")
	}
}

func TestRemoteLogsSavedPathsAndLivePanel(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	state.setAppearance(appearanceLight)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	started, stopped := make(chan string, 2), make(chan string, 2)
	ready := make(chan struct{})
	fake.streamCommand = func(ctx context.Context, command string, _ int, line func([]byte) bool) error {
		started <- command
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		}
		line([]byte("\n"))
		for i := range 30 {
			line(fmt.Appendf(nil, "INFO sample line %d\n", i))
		}
		line([]byte("ERROR payment endpoint\n"))
		<-ctx.Done()
		stopped <- command
		return ctx.Err()
	}
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Server logs", Width: 1200, Height: 800, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
		OnResize: state.Resize, OnWake: state.Wake,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	click := func(label string) {
		t.Helper()
		if err := view.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	typeText := func(label, value string) {
		t.Helper()
		click(label)
		if err := view.Type(value); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := view.Snapshot()
	var entry gfx.Rect
	for _, hit := range snapshot.Hits {
		if hit.ID == hitServerLogs {
			entry = hit.Bounds
		}
	}
	if rail := state.toolbarDockSideBoundsFor(state.viewport, false); entry.Width == 0 ||
		entry.X < rail.X || entry.X+entry.Width > rail.X+rail.Width ||
		entry.Y < rail.Y || entry.Y+entry.Height > rail.Y+rail.Height {
		t.Fatalf("log entry %v is not on the right rail %v", entry, rail)
	}
	click(state.messages.RemoteLogs.Title)
	if session.remoteLogs == nil {
		t.Fatal("the toolbar did not open the log panel")
	}
	collapsedHeight := state.workspacePixelBounds(state.viewport).Height
	click(state.messages.RemoteLogs.Expand)
	expandedHeight := state.workspacePixelBounds(state.viewport).Height
	if !session.remoteLogs.expanded || expandedHeight >= collapsedHeight {
		t.Fatal("expand icon did not increase the panel height")
	}
	click(state.messages.RemoteLogs.Restore)
	restoredHeight := state.workspacePixelBounds(state.viewport).Height
	if session.remoteLogs.expanded || restoredHeight <= expandedHeight ||
		math.Abs(float64(restoredHeight-collapsedHeight)) > 1 {
		t.Fatal("restore icon did not restore the original height")
	}
	split, err := view.Locate(state.messages.RemoteLogs.ResizePaths)
	if err != nil {
		t.Fatal(err)
	}
	x, y := split.Bounds.X+split.Bounds.Width/2, split.Bounds.Y+split.Bounds.Height/2
	if err := view.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x+130, y); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x+130, y); err != nil {
		t.Fatal(err)
	}
	wider, err := view.Locate(state.messages.RemoteLogs.ResizePaths)
	if err != nil || wider.Bounds.X < split.Bounds.X+120 {
		t.Fatalf("dragged splitter = %v, was %v, error %v", wider.Bounds, split.Bounds, err)
	}
	path := remoteLogPath{Name: "App log", Path: "/var/log/owner's app.log"}
	click("Add path")
	typeText("Name", path.Name)
	typeText("Remote absolute path", path.Path)
	click("Add path")
	if !slices.Equal(session.host.RemoteLogPaths, []remoteLogPath{path}) {
		t.Fatalf("live session paths = %v, want %v", session.host.RemoteLogPaths, path)
	}
	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range saved.Hosts {
		if host.ID != session.host.CatalogID && len(host.RemoteLogPaths) != 0 {
			t.Fatalf("paths leaked to %s", host.ID)
		}
	}
	reopened := newTestState(t, state.options)
	host, ok := reopened.hostByID(session.HostID)
	if !ok || !slices.Equal(host.RemoteLogPaths, []remoteLogPath{path}) {
		t.Fatalf("restored paths = %v, want %v", host.RemoteLogPaths, path)
	}
	click(path.Name + " " + path.Path)
	var command string
	select {
	case command = <-started:
	case <-time.After(time.Second):
		t.Fatal("selecting the row did not start its stream")
	}
	if !strings.Contains(command, "'/var/log/owner'\\''s app.log'") {
		t.Fatalf("remote command did not quote the path: %s", command)
	}
	liveRow := func() bool {
		snapshot := view.Snapshot()
		for _, hit := range snapshot.Hits {
			if hit.Label != path.Name+" "+path.Path+" · "+state.messages.RemoteLogs.Live {
				continue
			}
			for _, mark := range snapshot.Primitives {
				centerX, centerY := mark.Bounds.X+mark.Bounds.Width/2, mark.Bounds.Y+mark.Bounds.Height/2
				// The live marker is the small square success dot inside the row;
				// its exact size belongs to the table, not to the behavior.
				if mark.Kind == app.RectanglePrimitive && mark.Radius > 0 && mark.Color == state.pal.success &&
					mark.Bounds.Width > 0 && mark.Bounds.Width == mark.Bounds.Height &&
					mark.Bounds.Width < hit.Bounds.Height && pointInRect(centerX, centerY, hit.Bounds) {
					return true
				}
			}
		}
		return false
	}
	if liveRow() {
		t.Fatal("connecting row already shows a live indicator")
	}
	close(ready)
	deadline := time.Now().Add(time.Second)
	for len(session.remoteLogs.snapshot.lines) < 31 && time.Now().Before(deadline) {
		if err := view.Resize(1200, 800); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if len(session.remoteLogs.snapshot.lines) != 31 {
		t.Fatal("stream did not reach the visible panel")
	}
	foundBar := false
	var barWidth float32
	for _, hit := range view.Snapshot().Hits {
		if hit.Role == gfx.RoleSeparator && hit.Label == state.messages.RemoteLogs.Title && hit.Bounds.Width > 0 {
			foundBar, barWidth = true, hit.Bounds.Width
		}
	}
	if !foundBar || barWidth != 7 || session.remoteLogs.scroll == nil || session.remoteLogs.scroll.Max() <= 0 {
		t.Fatalf("the log output's scrollbar is not the old always-visible 7px named bar: found=%v width=%g max=%g",
			foundBar, barWidth, session.remoteLogs.scroll.Max())
	}
	if !liveRow() {
		t.Fatal("connected path has no green indicator")
	}
	assertSwitch := func(checked bool) gfx.Rect {
		t.Helper()
		snapshot := view.Snapshot()
		hit, err := view.Locate("Auto-scroll")
		if err != nil || hit.Role != gfx.RoleSwitch || hit.Checked != checked {
			t.Fatalf("auto-scroll switch hit = %+v, want checked=%v, error %v", hit, checked, err)
		}
		// The switch draws its track first and the thumb on top; paint order
		// tells them apart without pinning their size or radius.
		var track, thumb gfx.Rect
		shapes := 0
		for _, mark := range snapshot.Primitives {
			centerX, centerY := mark.Bounds.X+mark.Bounds.Width/2, mark.Bounds.Y+mark.Bounds.Height/2
			if mark.Kind != app.RectanglePrimitive || mark.Radius <= 0 || !pointInRect(centerX, centerY, hit.Bounds) {
				continue
			}
			if shapes == 0 {
				track = mark.Bounds
			}
			thumb = mark.Bounds
			shapes++
		}
		if shapes < 2 {
			t.Fatalf("auto-scroll switch drew %d rounded parts, want a track and a thumb", shapes)
		}
		if thumb.Width >= track.Width || thumb.Y < track.Y || thumb.Y+thumb.Height > track.Y+track.Height ||
			thumb.X < track.X || thumb.X+thumb.Width > track.X+track.Width {
			t.Fatalf("auto-scroll thumb %v is not inside its track %v", thumb, track)
		}
		trackCenterY := track.Y + track.Height/2
		caption := false
		for _, text := range snapshot.Texts {
			if text.Value != "Auto-scroll" {
				continue
			}
			centerY := text.Bounds.Y + text.Bounds.Height/2
			// The caption rides the same band as the track rather than an
			// exact pixel row.
			if centerY >= hit.Bounds.Y && centerY < hit.Bounds.Y+hit.Bounds.Height &&
				math.Abs(float64(centerY-trackCenterY)) < float64(track.Height/2) {
				caption = true
			}
		}
		if !caption {
			t.Fatalf("auto-scroll caption is not aligned with the switch track: hit=%v track=%v", hit.Bounds, track)
		}
		return thumb
	}
	rightThumb := assertSwitch(true)
	click("Auto-scroll")
	leftThumb := assertSwitch(false)
	if leftThumb.X >= rightThumb.X {
		t.Fatalf("unchecked thumb %v did not move left of checked thumb %v", leftThumb, rightThumb)
	}
	if !liveRow() {
		t.Fatal("pausing scroll cleared the live indicator")
	}
	if session.remoteLogs.follow {
		t.Fatal("pause left following enabled")
	}
	click("Auto-scroll")
	resumedThumb := assertSwitch(true)
	if resumedThumb.X <= leftThumb.X {
		t.Fatalf("resumed thumb %v did not move right of paused thumb %v", resumedThumb, leftThumb)
	}
	if !session.remoteLogs.follow {
		t.Fatal("auto-scroll did not resume following")
	}
	typeText("Find in log", "payment")
	if !slices.Equal(session.remoteLogs.matches, []int{30}) {
		t.Fatalf("search matches = %v", session.remoteLogs.matches)
	}
	other := remoteLogPath{Name: "Access log", Path: "/var/log/access.log"}
	click("Add path")
	typeText("Name", other.Name)
	typeText("Remote absolute path", other.Path)
	click("Add path")
	click("Actions for Access log")
	click("Remove path")
	click("Remove path")
	if !slices.Equal(session.host.RemoteLogPaths, []remoteLogPath{path}) || session.remoteLogs.selected != path || !liveRow() {
		t.Fatal("removing an inactive path changed the live stream")
	}
	click("Actions for App log")
	click("Edit path")
	typeText("Name", " v2")
	click("Edit path")
	if session.host.RemoteLogPaths[0].Name != "App log v2" {
		t.Fatalf("renamed paths = %v", session.host.RemoteLogPaths)
	}
	click("Close logs")
	if session.remoteLogs != nil {
		t.Fatal("the log panel stayed open")
	}
	select {
	case got := <-stopped:
		if got != command {
			t.Fatalf("stopped %q, want %q", got, command)
		}
	case <-time.After(time.Second):
		t.Fatal("closing the panel did not cancel the stream")
	}
	click("Server logs")
	click("App log v2 " + path.Path)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("reopening did not start the selected path")
	}
	click("Actions for App log v2")
	click("Remove path")
	click("Remove path")
	if len(session.host.RemoteLogPaths) != 0 || session.remoteLogs.buffer != nil {
		t.Fatal("removing the selected path kept the entry or its stream")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("removing the selected path did not cancel the stream")
	}
}

func TestRemoteLogsRetentionBoundsAndLineNumbers(t *testing.T) {
	buffer := new(remoteLogBuffer)
	for i := range remoteLogBufferLines + 8 {
		buffer.append(fmt.Appendf(nil, "line %d\n", i))
	}
	var snap remoteLogSnapshot
	buffer.snapshot(&snap)
	if snap.first != 8 || len(snap.lines) != remoteLogBufferLines || snap.lines[0] != "line 8" {
		t.Fatalf("retained %d lines starting at %d", len(snap.lines), snap.first)
	}
	large := []byte(strings.Repeat("x", remoteLogLineLimit-1))
	for range 40 {
		buffer.append(large)
	}
	buffer.snapshot(&snap)
	size := 0
	for _, line := range snap.lines {
		size += len(line)
	}
	if size > remoteLogBufferSize || snap.first+int64(len(snap.lines)) != remoteLogBufferLines+48 {
		t.Fatalf("retained %d bytes across %d lines starting at %d", size, len(snap.lines), snap.first)
	}
}

func TestClosingSSHBrowserKeepsRemoteLogsRunning(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	canceled := false
	session.remoteLogs = &remoteLogView{cancel: func() { canceled = true }}
	state.tabMenu.open = true
	state.tabMenu.workspace = state.deck.ActiveWorkspace()
	state.clickTabMenu(hitTabMenuBrowserClose)
	if canceled {
		t.Fatal("closing the SSH browser stopped the log stream")
	}
}
