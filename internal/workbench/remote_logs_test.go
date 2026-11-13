//go:build darwin || windows || linux

package workbench

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

func TestRemoteLogsSavedPathsAndLivePanel(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	started, stopped := make(chan string, 2), make(chan string, 2)
	fake.streamCommand = func(ctx context.Context, command string, _ int, line func([]byte) bool) error {
		started <- command
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
	click(state.messages.RemoteLogs.Title)
	if session.remoteLogs == nil {
		t.Fatal("the toolbar did not open the log panel")
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
	click(path.Name)
	var command string
	select {
	case command = <-started:
	case <-time.After(time.Second):
		t.Fatal("selecting the row did not start its stream")
	}
	if !strings.Contains(command, "'/var/log/owner'\\''s app.log'") {
		t.Fatalf("remote command did not quote the path: %s", command)
	}
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
	click("Pause")
	if session.remoteLogs.follow {
		t.Fatal("pause left following enabled")
	}
	typeText("Find in log", "payment")
	if !slices.Equal(session.remoteLogs.matches, []int{30}) {
		t.Fatalf("search matches = %v", session.remoteLogs.matches)
	}
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
	click("App log v2")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("reopening did not start the selected path")
	}
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
