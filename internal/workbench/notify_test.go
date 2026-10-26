package workbench

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconn"
)

type notifyWorld struct {
	*world
	options Options
	shown   []app.Notification
	clock   time.Duration
	fail    error
}

func newNotifyWorld(t testing.TB) *notifyWorld {
	known, options := newWorld(t)
	w := &notifyWorld{world: known}
	options.Notify = func(n app.Notification) error {
		w.shown = append(w.shown, n)
		return w.fail
	}
	options.Now = func() time.Duration { return w.clock }
	w.options = options
	return w
}

func (w *notifyWorld) session(t testing.TB, state *State) *fakeShell {
	t.Helper()
	fake := connect(t, state, w.world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	return fake.shell(t)
}

func TestAProgramsNotificationIsShownWithTheHostAndTheMessage(t *testing.T) {
	w := newNotifyWorld(t)
	state := newTestState(t, w.options)
	shell := w.session(t, state)
	shell.say("\x1b]777;notify;Backup;finished OK\x07")
	state.Wake()
	if len(w.shown) != 1 || w.shown[0].Body != "finished OK" || w.shown[0].Title != "Production · Backup" {
		t.Fatalf("shown = %+v", w.shown)
	}
	w.clock += 2 * time.Second
	shell.say("\x1b]9;plain message\x07")
	state.Wake()
	if len(w.shown) != 2 || w.shown[1].Title != "Production" || w.shown[1].Body != "plain message" {
		t.Fatalf("shown = %+v", w.shown)
	}
}

func TestANotificationLoopIsShownOnceASecondAtMost(t *testing.T) {
	w := newNotifyWorld(t)
	state := newTestState(t, w.options)
	shell := w.session(t, state)
	for range 50 {
		shell.say("\x1b]9;again\x07")
		state.Wake()
	}
	if len(w.shown) != 1 {
		t.Fatalf("%d notifications for a loop", len(w.shown))
	}
	w.clock += 1500 * time.Millisecond
	shell.say("\x1b]9;later\x07")
	state.Wake()
	if len(w.shown) != 2 {
		t.Fatalf("%d notifications after the interval", len(w.shown))
	}
}

func TestNotificationsCanBeTurnedOffAndStayOff(t *testing.T) {
	w := newNotifyWorld(t)
	state := newTestState(t, w.options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	state.Click(hitSettingsNotifyToggle)
	if !state.notifyOff {
		t.Fatal("the setting did not change")
	}
	state.Click(hitHosts)
	shell := w.session(t, state)
	shell.say("\x1b]9;nobody asked\x07")
	state.Wake()
	if len(w.shown) != 0 {
		t.Fatalf("shown = %+v", w.shown)
	}
	if again := newTestState(t, w.options); !again.notifyOff {
		t.Fatal("the choice was not kept")
	}
	state.Click(hitSettings)
	state.Click(hitSettingsNotifyToggle)
	if state.notifyOff {
		t.Fatal("turning it on did nothing")
	}
}

func TestAFailureToNotifyIsToldAndNeverStopsTheTerminal(t *testing.T) {
	w := newNotifyWorld(t)
	w.fail = errors.New("no notification service")
	state := newTestState(t, w.options)
	shell := w.session(t, state)
	shell.say("\x1b]9;hello\x07after")
	state.Wake()
	if state.status != "no notification service" {
		t.Fatalf("status = %q", state.status)
	}
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if got := session.term.screen.RowText(0); !strings.Contains(got, "after") {
		t.Fatal("the text after the sequence was lost")
	}
}
