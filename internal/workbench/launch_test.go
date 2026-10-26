package workbench

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/update"
)

func TestLaunchTextReadsWhatPeopleTypeAtACommandLine(t *testing.T) {
	for _, test := range []struct {
		args []string
		text string
		link bool
	}{
		{[]string{"alice@host"}, "alice@host", false},
		{[]string{"-ssh", "-P", "2222", "-l", "bob", "host"}, "-p 2222 -l bob host", false},
		{[]string{"-ssh", "-P2200", "host"}, "-p2200 host", false},
		{[]string{"ssh://bob@host:2222/"}, "ssh://bob@host:2222/", true},
		{[]string{"TELNET://switch"}, "TELNET://switch", true},
		{[]string{"--background"}, "", false},
		{nil, "", false},
	} {
		text, link := launchText(test.args)
		if text != test.text || link != test.link {
			t.Errorf("launchText(%q) = %q, %v; want %q, %v", test.args, text, link, test.text, test.link)
		}
	}
	// What the PuTTY-style words turn into must parse to the target meant.
	text, _ := launchText([]string{"-ssh", "-P", "2222", "-l", "bob", "host"})
	if parsed, err := parseConnection(text); err != nil || parsed.user != "bob" || parsed.host != "host" || parsed.port != 2222 {
		t.Fatalf("PuTTY words became %+v, %v", parsed, err)
	}
}

func launchState(t *testing.T) (*State, *world, *int) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	raised := 0
	options.Raise = func() { raised++ }
	state := newTestState(t, options)
	state.Wake()
	return state, world, &raised
}

func TestACommandLineTargetConnectsAtOnce(t *testing.T) {
	state, world, raised := launchState(t)
	state.Launch([]string{"-ssh", "-P", "2222", "-l", "alice", "new.example.com"})
	state.Wake()
	if len(world.configs) != 1 {
		t.Fatalf("dialed %d connections", len(world.configs))
	}
	config := world.configs[0]
	if config.Host != "new.example.com" || config.Port != 2222 || config.User != "alice" {
		t.Fatalf("dialed %+v", config)
	}
	if *raised != 1 {
		t.Fatalf("the window was raised %d times", *raised)
	}
}

func TestALinkAsksBeforeConnecting(t *testing.T) {
	state, world, _ := launchState(t)
	state.Launch([]string{"ssh://bob@link.example.com:2200/"})
	state.Wake()
	if state.modal != modalOpenLink || len(world.configs) != 0 {
		t.Fatalf("modal %d, dials %d", state.modal, len(world.configs))
	}
	_, texts := paintedTexts(state)
	if !contains(texts, "bob@link.example.com") || !contains(texts, state.messages.Tools.LaunchConnect) {
		t.Fatalf("the dialog does not name the target: %q", texts)
	}
	state.Click(hitLaunchCancel)
	if state.modal != modalNone || len(world.configs) != 0 {
		t.Fatalf("cancel: modal %d, dials %d", state.modal, len(world.configs))
	}
	state.Launch([]string{"ssh://bob@link.example.com:2200/"})
	state.Wake()
	state.Click(hitLaunchConnect)
	if len(world.configs) != 1 || world.configs[0].Host != "link.example.com" || world.configs[0].Port != 2200 || world.configs[0].User != "bob" {
		t.Fatalf("dials %+v", world.configs)
	}
}

func TestATelnetLinkOpensATelnetSessionAfterAsking(t *testing.T) {
	state, _, _ := launchState(t)
	state.Launch([]string{"telnet://router.example.com:2323"})
	state.Wake()
	if state.modal != modalOpenLink || state.launchTarget.protocol != protocolTelnet || state.launchTarget.port != 2323 {
		t.Fatalf("modal %d target %+v", state.modal, state.launchTarget)
	}
	if d := state.launchDescription(); !strings.Contains(d, "Telnet") || strings.Contains(d, "telnet://") || !strings.Contains(d, "router.example.com:2323") {
		t.Fatalf("description %q", state.launchDescription())
	}
}

func TestABadTargetIsReportedAndConnectsNowhere(t *testing.T) {
	state, world, _ := launchState(t)
	state.Launch([]string{"--evil", "-oProxyCommand=x", "host"})
	state.Wake()
	if len(world.configs) != 0 || !strings.Contains(state.status, "evil") {
		t.Fatalf("dials %d, status %q", len(world.configs), state.status)
	}
}

func TestRequestsWaitForADialogToClose(t *testing.T) {
	state, world, _ := launchState(t)
	state.modal = modalMoveHostGroup
	state.Launch([]string{"late@host.example.com"})
	state.Wake()
	if len(world.configs) != 0 {
		t.Fatal("a request ran under an open dialog")
	}
	state.modal = modalNone
	state.Wake()
	if len(world.configs) != 1 {
		t.Fatalf("dials %d after the dialog closed", len(world.configs))
	}
}

func TestSeveralRequestsAreAllServedInOrder(t *testing.T) {
	state, world, _ := launchState(t)
	state.Launch([]string{"a@one.example.com"})
	state.Launch([]string{"b@two.example.com"})
	state.Wake()
	var hosts []string
	for _, config := range world.configs {
		hosts = append(hosts, config.Host)
	}
	if !reflect.DeepEqual(hosts, []string{"one.example.com", "two.example.com"}) {
		t.Fatalf("dialed %q", hosts)
	}
}

func openGeneralSettings(state *State) {
	state.activeSection = sectionSettings
	state.settingsCategory = settingsGeneral
}

func waitUpdate(t *testing.T, state *State, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the update check did not finish")
}

func TestCheckingForUpdatesOffersTheDownloadOfANewerVersion(t *testing.T) {
	world, options := newWorld(t)
	_ = world
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{Version: "99.0.0", URL: "https://example.com/release"}, nil
	}
	var opened []string
	options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state := newTestState(t, options)
	openGeneralSettings(state)
	_, texts := paintedTexts(state)
	if contains(texts, state.messages.Tools.GeneralUpdateDownload) {
		t.Fatal("a download button before any check")
	}
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.update.available })
	_, texts = paintedTexts(state)
	if !contains(texts, "99.0.0") || !contains(texts, state.messages.Tools.GeneralUpdateDownload) {
		t.Fatalf("the result is not shown: %q", texts)
	}
	state.Click(hitSettingsUpdateDownload)
	if !reflect.DeepEqual(opened, []string{"https://example.com/release"}) {
		t.Fatalf("opened %q", opened)
	}
}

func TestCheckingForUpdatesSaysSoWhenCurrentOrFailed(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	answer := update.Release{Version: update.Current, URL: "https://example.com/r"}
	var failure error
	options.CheckUpdate = func() (update.Release, error) { return answer, failure }
	state := newTestState(t, options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return !state.update.checking })
	if state.update.available || !strings.Contains(state.update.text, update.Current) {
		t.Fatalf("current: %+v", state.update)
	}
	failure = errors.New("offline")
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return !state.update.checking })
	if !strings.Contains(state.update.text, "offline") {
		t.Fatalf("failed: %q", state.update.text)
	}
}

func TestTheStartupCheckIsOptInAndRemembered(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	calls := make(chan struct{}, 4)
	options.CheckUpdate = func() (update.Release, error) {
		calls <- struct{}{}
		return update.Release{Version: "99.0.0", URL: "https://example.com/r"}, nil
	}
	state := New(options)
	t.Cleanup(state.Close)
	state.Wake()
	select {
	case <-calls:
		t.Fatal("a check ran without being asked for")
	case <-time.After(50 * time.Millisecond):
	}
	state.Click(hitSettingsUpdateStartToggle)
	state.Close()

	again := New(options)
	defer again.Close()
	if !again.updateAtStart {
		t.Fatal("the choice was not saved")
	}
	waitUpdate(t, again, func() bool { return again.update.available })
	if !strings.Contains(again.status, "99.0.0") {
		t.Fatalf("status %q does not announce the version", again.status)
	}
}

func TestStartAtLoginFollowsTheSystemAndReportsFailure(t *testing.T) {
	_, options := newWorld(t)
	on := false
	var failure error
	options.StartAtLogin = &LoginStart{
		Enabled: func() (bool, error) { return on, nil },
		Set: func(want bool) error {
			if failure == nil {
				on = want
			}
			return failure
		},
	}
	state := newTestState(t, options)
	openGeneralSettings(state)
	_, texts := paintedTexts(state)
	if !contains(texts, state.messages.Tools.GeneralLogin) {
		t.Fatalf("no login row: %q", texts)
	}
	state.Click(hitSettingsLoginToggle)
	if !on || !state.loginOn {
		t.Fatalf("on %v, shown %v", on, state.loginOn)
	}
	failure = errors.New("denied")
	state.Click(hitSettingsLoginToggle)
	if !on || !state.loginOn || !strings.Contains(state.status, "denied") {
		t.Fatalf("a failed change was shown as done: on %v shown %v status %q", on, state.loginOn, state.status)
	}
}

func TestWithoutALoginHookTheSettingIsAbsent(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openGeneralSettings(state)
	_, texts := paintedTexts(state)
	if contains(texts, state.messages.Tools.GeneralLogin) {
		t.Fatal("a login row without a way to change it")
	}
}

func TestAWaitingRequestStartsWhenTheDialogIsClosedByAKey(t *testing.T) {
	state, world, _ := launchState(t)
	state.Launch([]string{"ssh://one@one.example.com/"})
	state.Wake() // the link asks first
	state.Launch([]string{"two@two.example.com"})
	state.Wake() // and the second waits behind the question
	if len(world.configs) != 0 || state.modal != modalOpenLink {
		t.Fatalf("modal %d, dials %d", state.modal, len(world.configs))
	}
	// Closing the dialog is an event, not a wake: the request must not wait for
	// something else to wake the window.
	state.Click(hitLaunchCancel)
	if len(world.configs) != 1 || world.configs[0].Host != "two.example.com" {
		t.Fatalf("dials %+v", world.configs)
	}
	// A key that closes a dialog asks for the repaint the new tab needs.
	state.Launch([]string{"ssh://three@three.example.com/"})
	state.Wake()
	state.Launch([]string{"four@four.example.com"})
	state.Wake()
	state.modal = modalNone // as an escape or a click elsewhere would
	if result := state.Key(ui.KeyEvent{Key: ui.KeyTab}); !result.Repaint || len(world.configs) != 2 {
		t.Fatalf("repaint %v, dials %d", result.Repaint, len(world.configs))
	}
}

func TestAMissingReleaseIsSaidPlainlyNotAsAFailure(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) { return update.Release{}, update.ErrNoRelease }
	state := newTestState(t, options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return !state.update.checking })
	if state.update.text != state.messages.Tools.UpdateNoReleaseText || state.update.available {
		t.Fatalf("text %q, available %v", state.update.text, state.update.available)
	}
}
