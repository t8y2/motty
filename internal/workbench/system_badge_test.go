package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func TestTabPaintsSystemBeforeTitleAndStatusAfterIt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.System = sshconn.SystemUbuntu
	})
	state.Wake()
	canvas, _ := paintedTexts(state)

	var title ui.Text
	for _, text := range canvas.Texts() {
		if text.Value == state.sessions[0].Title && text.Bounds.Y < windowBarHeight {
			title = text
			break
		}
	}
	if title.Value == "" {
		t.Fatal("tab title was not painted")
	}
	leading, _ := app.TitlebarInsets()
	barStart := leading + barMargin
	if title.Bounds.X <= barStart+20 {
		t.Fatalf("title starts at %.1f, before the system badge", title.Bounds.X)
	}
	// A connected session's light is a solid dot, with no halo around it, in the
	// middle of the bar and clear of the title it follows.
	var light ui.Rectangle
	var ok bool
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Width == lightCore && rectangle.Bounds.X >= title.Bounds.X+title.Bounds.Width && rectangle.Bounds.Y == windowBarHeight/2-lightCore/2 {
			light, ok = rectangle, true
			break
		}
	}
	if !ok || light.Bounds.X <= title.Bounds.X {
		t.Fatalf("status light = %+v, want it after title %+v", light, title.Bounds)
	}
}

func TestTabShowsTheIconChosenForTheHostNotTheDetectedSystem(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.hosts[0].Icon = hostIconOf(sshconn.SystemUbuntu)
	fake := connect(t, state, world)
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.System = sshconn.SystemDebian
	})
	state.Wake()
	canvas, _ := paintedTexts(state)

	ubuntu, _ := systemBadgeArt(sshconn.SystemUbuntu, state.pal.dark)
	debian, _ := systemBadgeArt(sshconn.SystemDebian, state.pal.dark)
	var sawUbuntu, sawDebian bool
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Bounds.Y >= windowBarHeight {
			continue
		}
		sawUbuntu = sawUbuntu || rectangle.Color == ubuntu.background
		sawDebian = sawDebian || rectangle.Color == debian.background
	}
	if !sawUbuntu || sawDebian {
		t.Fatalf("tab badge: ubuntu=%v debian=%v, want the host's chosen Ubuntu icon and not the detected Debian one", sawUbuntu, sawDebian)
	}
}
