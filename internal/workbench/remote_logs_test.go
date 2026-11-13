//go:build darwin || windows || linux

package workbench

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

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
		entry.Y < rail.Y+rail.Height/2 || entry.Y+entry.Height > rail.Y+rail.Height {
		t.Fatalf("log entry %v is not anchored to the bottom of the right rail %v", entry, rail)
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
