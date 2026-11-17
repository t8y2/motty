package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"motty/internal/sshconn"
)

// The local proxy dialog once the tunnel is up: the port it chose is random, so a number of four or
// five digits inside a text or label is normalized (coordinates are not touched).
var portNumbers = regexp.MustCompile(`\b[1-9][0-9]{3,4}\b`)

func dumpWithoutPorts(canvas *gfx.Canvas) string {
	var b strings.Builder
	for _, r := range canvas.Rectangles() {
		fmt.Fprintf(&b, "rect %v\n", r)
	}
	for _, s := range canvas.Shadows() {
		fmt.Fprintf(&b, "shadow %v\n", s)
	}
	for _, x := range canvas.Texts() {
		fmt.Fprintf(&b, "text %q %v\n", portNumbers.ReplaceAllString(x.Value, "PORT"), x.Bounds)
	}
	for _, h := range canvas.Hits() {
		line := fmt.Sprintf("hit %v %q %v %v disabled=%v\n", h.ID, portNumbers.ReplaceAllString(h.Label, "PORT"), h.Role, h.Bounds, h.Disabled)
		b.WriteString(hitNumbers.ReplaceAllString(line, "hit "))
	}
	return b.String()
}

func TestPaintGoldenLocalProxyConnected(t *testing.T) {
	for _, view := range []string{"main", "more", "docker"} {
		name := "local-proxy-connected-" + view
		t.Run(name, func(t *testing.T) {
			world, options := newWorld(t)
			connection := &fakeConn{}
			world.conns = append(world.conns, connection)
			connection.listen = net.Listen
			connection.exec = func(_ context.Context, command string, _ int) (sshconn.ExecResult, error) {
				raw := strings.TrimSuffix(command[strings.LastIndex(command, ":")+1:], "'")
				return sshconn.ExecResult{Output: []byte(fmt.Sprintf("LISTEN 0 128 127.0.0.1:%s 0.0.0.0:*\n", raw))}, nil
			}
			// Never the real clipboard: starting the tunnel copies a command. A failing one keeps the
			// dialog on the connected panel, with the copy error under it, as the dialog's own tests do.
			options.SetClipboardText = func(string) error { return errors.New("clipboard unavailable") }
			options.Dial = func(sshconn.Config) Connection {
				return &localProxyTestLender{fakeConn: connection, shared: connection}
			}
			state := newTestState(t, options)
			openHost(state)
			connection.set(func(status *sshconn.Status) {
				status.Phase, status.ConnectionGeneration = sshconn.PhaseConnected, 9001
			})
			state.Wake()
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			state.openLocalProxy(session)
			port, closeProxy := localProxyTestPort(t)
			defer closeProxy()
			state.localProxyUI.port = text.Editor{}
			state.localProxyUI.port.Insert(strconv.Itoa(int(port)))
			target, _ := state.localProxyTarget(session)
			state.Click(hitLocalProxyStart)
			waitLocalProxyPhase(t, state.localProxyManager(), target, localProxyConnected)
			state.Wake()
			if state.modal != modalLocalProxy {
				t.Fatalf("the dialog closed after connecting (modal %d)", state.modal)
			}
			switch view {
			case "more":
				state.localProxyUI.more = true
			case "docker":
				state.localProxyUI.docker = true
			}
			var canvas gfx.Canvas
			state.Paint(&canvas, paintDesktop)
			got := dumpWithoutPorts(&canvas)
			path := filepath.Join("testdata", "paint", name+".txt")
			if os.Getenv("UPDATE_PAINT_GOLDEN") != "" {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal("no golden: run with UPDATE_PAINT_GOLDEN=1")
			}
			if got != string(want) {
				t.Errorf("paints differently from its golden\n%s", firstDifference(string(want), got))
			}
		})
	}
}
