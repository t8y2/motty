package rdp

import (
	"context"
	"errors"
	"net"
	"testing"

	"motty/internal/desktop"
	rdpcore "motty/internal/rdpcore"
)

// Standard RDP security has no certificate step to ask the password at, so the
// question is asked before the connection is opened. A cancelled question must
// not dial.
func TestStandardSecurityAsksForThePasswordBeforeDialing(t *testing.T) {
	cancelled := errors.New("cancelled")
	var asked bool
	dial := func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed before the password question was answered")
		return nil, nil
	}
	_, err := Open(context.Background(), dial, "127.0.0.1:3389", desktop.Options{
		Username: "alice", StandardSecurity: true,
		AskPassword: func() (string, error) {
			asked = true
			return "", cancelled
		},
	})
	if !asked {
		t.Fatal("the password was not asked for")
	}
	if !errors.Is(err, cancelled) {
		t.Fatalf("open error = %v, want the cancelled question", err)
	}
}

func TestQualityOptionsReachTheRDPClient(t *testing.T) {
	apply := func(opts desktop.Options) *rdpcore.Options {
		config := rdpcore.DefaultOptions()
		applyQuality(config, opts)
		return config
	}
	// A host that never chose keeps what a session has always had.
	config := apply(desktop.Options{})
	if config.Depth != 32 || !config.GFX {
		t.Errorf("default: depth %d, GFX %v", config.Depth, config.GFX)
	}
	if config.Wallpaper || config.FullWindowDrag || config.MenuAnimations || config.DesktopComposition ||
		!config.Theming || !config.CursorShadow || !config.FontSmoothing {
		t.Errorf("default effects: %+v", config)
	}
	// A lower depth is only honoured without the graphics pipeline.
	for _, depth := range []int{24, 16} {
		config := apply(desktop.Options{ColorDepth: depth})
		if int(config.Depth) != depth || config.GFX {
			t.Errorf("depth %d: got depth %d, GFX %v", depth, config.Depth, config.GFX)
		}
	}
	// 8 is not an RDP choice of this client: it stays at 32.
	if config := apply(desktop.Options{ColorDepth: 8}); config.Depth != 32 || !config.GFX {
		t.Errorf("depth 8: got depth %d, GFX %v", config.Depth, config.GFX)
	}
	// Every effect has its own switch, both ways.
	config = apply(desktop.Options{Effects: &desktop.Effects{Wallpaper: true, WindowDrag: true, Animations: true, Composition: true}})
	if !config.Wallpaper || !config.FullWindowDrag || !config.MenuAnimations || !config.DesktopComposition ||
		config.Theming || config.CursorShadow || config.FontSmoothing {
		t.Errorf("custom effects: %+v", config)
	}
}
