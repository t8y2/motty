package workbench

import (
	"testing"
	"time"
)

func TestTerminalBellSoundsOncePerBurstAndCanBeSilenced(t *testing.T) {
	now := time.Second
	rings := 0
	options := testOptions(t)
	options.Now = func() time.Duration { return now }
	options.Bell = func() error { rings++; return nil }
	state := newTestState(t, options)
	openHost(state)
	screen := state.sessions[0].term.screen

	screen.Write([]byte("\a\a\a"))
	if rings != 1 {
		t.Fatalf("a burst of bells sounded %d times", rings)
	}
	now += bellInterval
	screen.Write([]byte("\a"))
	if rings != 2 {
		t.Fatalf("a bell after the interval sounded %d times in total", rings)
	}

	state.terminalBellOff = true
	now += time.Minute
	screen.Write([]byte("\a"))
	if rings != 2 {
		t.Fatalf("a silenced bell sounded: %d", rings)
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
