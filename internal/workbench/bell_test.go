package workbench

import (
	"testing"
	"time"
)

func TestTerminalBellSoundsOncePerBurstAndCanBeSilenced(t *testing.T) {
	now := time.Second
	calls := make(chan struct{}, 4)
	release := make(chan struct{})
	options := testOptions(t)
	options.Now = func() time.Duration { return now }
	// The player blocks, the way the system alert sound does. The write has to
	// return while it is still blocked, or a binary file of BEL holds the window.
	options.Bell = func() error {
		calls <- struct{}{}
		<-release
		return nil
	}
	state := newTestState(t, options)
	openHost(state)
	screen := state.sessions[0].term.screen

	screen.Write([]byte("\a\a\a"))
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("a burst of bells never sounded")
	}
	select {
	case <-calls:
		t.Fatal("a burst of bells sounded more than once")
	default:
	}
	close(release)

	now += bellInterval
	screen.Write([]byte("\a"))
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("a bell after the interval did not sound")
	}

	state.terminalBellOff = true
	now += time.Minute
	screen.Write([]byte("\a"))
	select {
	case <-calls:
		t.Fatal("a silenced bell sounded")
	default:
	}
}

func TestBellAndLinkSettingsSurviveRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.terminalBellOff = true
	state.terminalLinkPlainClick = true
	state.save()
	again := newTestState(t, options)
	if !again.terminalBellOff || !again.terminalLinkPlainClick {
		t.Fatalf("after a restart bell off = %t, plain-click links = %t", again.terminalBellOff, again.terminalLinkPlainClick)
	}

	fresh := newTestState(t, testOptions(t))
	if fresh.terminalBellOff || fresh.terminalLinkPlainClick {
		t.Fatal("a new profile must ring the bell and need the modifier to open a link")
	}
}
