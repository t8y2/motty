package workbench

import (
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
)

// keyRecorder is a remote desktop that records the keys it is sent.
type keyRecorder struct {
	*followingDesktop
	mu   sync.Mutex
	keys []desktop.KeyEvent
}

func (r *keyRecorder) SendKey(event desktop.KeyEvent) error {
	r.mu.Lock()
	r.keys = append(r.keys, event)
	r.mu.Unlock()
	return nil
}

func (r *keyRecorder) take() []desktop.KeyEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := r.keys
	r.keys = nil
	return keys
}

// While a remote desktop holds the keyboard, Alt+Tab belongs to it wherever in Motty's window the
// user last clicked: a click on a tab, a toolbar button or bare ground used to move the focus off
// the picture, which turned Alt+Tab into a Tab through Motty's own controls and kept the keys from
// the remote.
func TestAltTabStaysWithTheDesktopAfterClickingMottysControls(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	state.sessions[0].term.live = true
	remote := &keyRecorder{followingDesktop: newFollowingDesktop(800, 600, false)}
	state.sessions[0].desk = newDesktopView(remote, false)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	session := &state.sessions[0]
	picture := hitOf(t, driver, hitTerminalList.At(int(session.ID)))
	press := func(x, y float32) {
		t.Helper()
		if err := driver.Click(x, y); err != nil {
			t.Fatal(err)
		}
	}
	press(picture.Bounds.X+10, picture.Bounds.Y+10)

	labelled := func(label string) gfx.Hit {
		t.Helper()
		for _, hit := range driver.Snapshot().Hits {
			if hit.Label == label && hit.Bounds.Y < 80 {
				return hit
			}
		}
		t.Fatalf("no %q control painted", label)
		return gfx.Hit{}
	}
	center := func(hit gfx.Hit) (float32, float32) {
		return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2
	}
	for _, test := range []struct {
		name  string
		click func()
	}{
		{"the picture", func() { press(picture.Bounds.X+10, picture.Bounds.Y+10) }},
		{"a bar button", func() { press(center(labelled("Fit"))) }},
		{"the tab", func() { press(center(labelled("Production"))) }},
		{"a toolbar button", func() { press(center(labelled("AI assistant"))) }},
		{"bare ground in the tab strip", func() { press(800, 18) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.click()
			if got := driver.Snapshot().Focused; got != hitTerminalList.At(int(session.ID)) {
				t.Fatalf("focus = %v after the click, want the desktop", got)
			}
			remote.take()
			for _, step := range []func() error{
				func() error { return driver.KeyDown(gfx.KeyAltLeft, "") },
				func() error { return driver.KeyDown(gfx.KeyTab, "") },
				func() error { return driver.KeyUp(gfx.KeyTab, "") },
				func() error { return driver.KeyUp(gfx.KeyAltLeft, "") },
			} {
				if err := step(); err != nil {
					t.Fatal(err)
				}
			}
			var tab, alt int
			for _, key := range remote.take() {
				switch key.Key {
				case desktop.KeyTab:
					tab++
				case desktop.KeyAlt:
					alt++
				}
			}
			if tab != 2 || alt != 2 {
				t.Fatalf("remote saw Tab x%d and Alt x%d (press and release each), want 2 and 2", tab, alt)
			}
		})
	}
}
