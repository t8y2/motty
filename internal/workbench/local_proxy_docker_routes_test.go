package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"motty/internal/sshconn"
)

// The Docker view's three copy/open buttons, clicked through the real window, with the tunnel up.
// The clipboard and the browser are fakes: the real ones are never touched.
func TestLocalProxyDockerButtonsReachTheirActions(t *testing.T) {
	world, options := newWorld(t)
	connection := &fakeConn{}
	world.conns = append(world.conns, connection)
	connection.listen = net.Listen
	connection.exec = func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
		raw := strings.TrimSuffix(command[strings.LastIndex(command, ":")+1:], "'")
		return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n", raw))}, nil
	}
	options.Dial = func(sshconn.Config) Connection {
		return &localProxyTestLender{fakeConn: connection, shared: connection}
	}
	var clipboard []string
	failFirst := true
	options.SetClipboardText = func(value string) error {
		if failFirst {
			return errors.New("clipboard unavailable") // keeps the dialog on the connected panel
		}
		clipboard = append(clipboard, value)
		return nil
	}
	var opened []string
	options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state := newTestState(t, options)
	openHost(state)
	connection.set(func(status *sshconn.Status) { status.Phase, status.ConnectionGeneration = sshconn.PhaseConnected, 9001 })
	state.Wake()
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.openLocalProxy(session)
	port, closeProxy := localProxyTestPort(t)
	defer closeProxy()
	state.localProxyUI.port = text.Editor{}
	state.localProxyUI.port.Insert(strconv.Itoa(int(port)))
	target, _ := state.localProxyTarget(session)
	state.Click(hitLocalProxyStart)
	snapshot := waitLocalProxyPhase(t, state.localProxyManager(), target, localProxyConnected)
	state.Wake()
	failFirst = false
	state.localProxyUI.docker = true

	view := openWorkbenchSession(t, state)
	click := func(id gfx.ID) {
		t.Helper()
		for _, h := range view.Snapshot().Hits {
			if h.ID == id && !h.Block {
				if err := view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("no control %v in the window", id)
	}
	click(hitLocalProxyDockerConfig)
	if len(clipboard) != 1 || clipboard[0] != localProxyDockerConfigCommand(snapshot.RemotePort) {
		t.Fatalf("the config button copied %q", clipboard)
	}
	click(hitLocalProxyDockerDocs)
	if len(opened) != 1 || opened[0] != dockerProxyDocsURL {
		t.Fatalf("the docs button opened %q", opened)
	}
	clipboard = nil
	click(hitLocalProxyDockerRestore)
	if len(clipboard) != 1 || clipboard[0] != localProxyDockerRestoreCommand() {
		t.Fatalf("the restore button copied %q", clipboard)
	}
}
