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
	if rail := state.toolbarDockSideBoundsFor(state.viewport, false); entry.Width == 0 || entry.X < rail.X || entry.Y+entry.Height != rail.Y+rail.Height-4 {
		t.Fatalf("log entry %v is not anchored to the bottom of the right rail %v", entry, rail)
	}
	click(state.messages.RemoteLogs.Title)
	if session.remoteLogs == nil {
		t.Fatal("the toolbar did not open the log panel")
	}
	terminalHeight := state.workspacePixelBounds(state.viewport).Height
	click(state.messages.RemoteLogs.Expand)
	if !session.remoteLogs.expanded || state.workspacePixelBounds(state.viewport).Height >= terminalHeight {
		t.Fatal("expand icon did not increase the panel height")
	}
	click(state.messages.RemoteLogs.Restore)
	if session.remoteLogs.expanded || state.workspacePixelBounds(state.viewport).Height != terminalHeight {
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
		for _, hit := range view.Snapshot().Hits {
			if hit.Label == path.Name+" "+path.Path+" · "+state.messages.RemoteLogs.Live {
				for _, mark := range view.Snapshot().Primitives {
					if mark.Color == state.pal.success && mark.Bounds.Width == 7 && mark.Bounds.Height == 7 &&
						pointInRect(mark.Bounds.X, mark.Bounds.Y, hit.Bounds) {
						return true
					}
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
	assertSwitch := func(checked bool) {
		t.Helper()
		snapshot := view.Snapshot()
		hit, err := view.Locate("Auto-scroll")
		if err != nil || hit.Role != gfx.RoleSwitch || hit.Checked != checked {
			t.Fatalf("auto-scroll switch hit = %+v, want checked=%v, error %v", hit, checked, err)
		}
		track := gfx.Rect{X: float32(math.Round(float64(hit.Bounds.X))), Y: float32(math.Round(float64(hit.Bounds.Y + 5))), Width: 32, Height: 18}
		thumb := gfx.Rect{X: track.X + 2, Y: track.Y + 2, Width: 14, Height: 14}

		if checked {
			thumb.X += 14

		}

		var hasTrack, hasThumb, hasCaption bool
		for _, mark := range snapshot.Primitives {
			hasTrack = hasTrack || mark.Bounds == track && mark.Radius == 9
			hasThumb = hasThumb || mark.Bounds == thumb && mark.Radius == 7
		}
		for _, text := range snapshot.Texts {
			hasCaption = hasCaption || text.Value == "Auto-scroll" && text.Bounds.Y+text.Bounds.Height/2 == hit.Bounds.Y+hit.Bounds.Height/2
		}
		if !hasTrack || !hasThumb || !hasCaption {
			t.Fatalf("auto-scroll alignment: hit=%v track=%v thumb=%v caption=%v", hit.Bounds, hasTrack, hasThumb, hasCaption)
		}
	}
	assertSwitch(true)
	click("Auto-scroll")
	assertSwitch(false)
	if !liveRow() {
		t.Fatal("pausing scroll cleared the live indicator")
	}
	if session.remoteLogs.follow {
		t.Fatal("pause left following enabled")
	}
	click("Auto-scroll")
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
