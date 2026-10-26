//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestHeadlessIdleMultiPaneWorkspaceDoesNotRepaint(t *testing.T) {
	state := threeVisiblePaneState(t)
	for index := range state.sessions {
		state.sessions[index].phase = sshconn.PhaseConnected
		state.sessions[index].term.live = true
	}
	paints := 0
	session, err := headless.Open(app.Config{
		Title: "idle workspace", Width: 1400, Height: 900,
		Paint: func(canvas *ui.Canvas, size ui.Size) {
			paints++
			state.Paint(canvas, size)
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	before := paints
	for range 100 {
		if err := session.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if paints != before {
		t.Fatalf("idle workspace repainted %d times", paints-before)
	}
}
