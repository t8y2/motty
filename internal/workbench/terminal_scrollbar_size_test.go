package workbench

import (
	"testing"

	"github.com/vivaui/viva/base"
)

func TestTerminalScrollbarSizePixelsAndGeometry(t *testing.T) {
	state, session, geometry := historyState()

	cases := []struct {
		size scrollbarSize
		want float32
	}{
		{scrollbarWide, base.ScrollAreaBarSize},
		{scrollbarMedium, 6},
		{scrollbarThin, 4},
	}
	for _, tc := range cases {
		state.terminalScrollbarSize = tc.size
		if got := tc.size.pixels(); got != tc.want {
			t.Fatalf("%s pixels = %v, want %v", tc.size, got, tc.want)
		}
		geom := state.terminalScrollGeometry(session, geometry.area)
		if geom.VerticalTrack.Width != tc.want {
			t.Fatalf("%s VerticalTrack.Width = %v, want %v", tc.size, geom.VerticalTrack.Width, tc.want)
		}
	}
}

func TestTerminalScrollbarSizePersistsRoundTrip(t *testing.T) {
	opts := testOptions(t)
	state := newTestState(t, opts)
	state.terminalScrollbarSize = scrollbarThin
	state.save()
	state.Close()

	reloaded := newTestState(t, opts)
	if reloaded.terminalScrollbarSize != scrollbarThin {
		t.Fatalf("reloaded terminalScrollbarSize = %v, want thin", reloaded.terminalScrollbarSize)
	}
}
