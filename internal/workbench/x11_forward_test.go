package workbench

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"motty/internal/sshconn"
	"motty/internal/x11fwd"
)

func TestForwardX11IsOffUntilSwitchedOnAndSurvivesARestart(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.editor.label.Insert("Display box")
	state.editor.address.Insert("display.example.com")
	state.Click(hitDialogSubmit)
	find := func(s *State) Host {
		for _, host := range s.hosts {
			if host.Label == "Display box" {
				return host
			}
		}
		t.Fatal("host missing")
		return Host{}
	}
	if find(state).ForwardX11 {
		t.Fatal("a new host forwards X11")
	}

	state.selectedHostID = find(state).ID
	state.openEditHost()
	state.Click(hitDialogForwardX11)
	if !state.editor.forwardX11 {
		t.Fatal("the switch did not turn on")
	}
	state.Click(hitDialogSubmit)
	if !find(state).ForwardX11 {
		t.Fatal("the choice was not saved")
	}
	again := New(state.options)
	defer again.Close()
	if !find(again).ForwardX11 {
		t.Fatal("the choice did not survive a restart")
	}
}

func TestX11ReasonTellsTheUserWhatToInstallOnTheirSystem(t *testing.T) {
	state := newTestState(t, testOptions(t))
	tools := state.messages.Tools
	unreachable := &x11fwd.UnreachableError{Address: "127.0.0.1:6000", Err: errors.New("refused")}
	cases := []struct {
		err  error
		goos string
		want []string
	}{
		{x11fwd.ErrNoDisplay, "darwin", []string{tools.X11HintMac}},
		{unreachable, "windows", []string{"127.0.0.1:6000", tools.X11HintWindows}},
		{unreachable, "linux", []string{tools.X11HintOther}},
		{fmt.Errorf("wrapped: %w", sshconn.ErrX11Refused), "linux", []string{"xauth"}},
	}
	for _, c := range cases {
		got := state.x11Reason(c.err, c.goos)
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%v on %s = %q, want it to contain %q", c.err, c.goos, got, want)
			}
		}
	}
}
