package workbench

import (
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
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
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	state.Click(hitUpdateLater)
	if state.modal != modalNone {
		t.Fatal("Later left the dialog open")
	}
	_, texts = paintedTexts(state)
	if !contains(texts, "99.0.0") || !contains(texts, state.messages.Tools.GeneralUpdateDownload) {
		t.Fatalf("the result is not shown: %q", texts)
	}
	state.Click(hitSettingsUpdateDownload)
	if !reflect.DeepEqual(opened, []string{"https://example.com/release"}) {
		t.Fatalf("opened %q", opened)
	}
}

func TestANewVersionIsOfferedInADialogWithItsNotesAsMarkdown(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{Version: "99.0.0", URL: "https://example.com/release", Notes: "## Fixed\n- the thing"}, nil
	}
	var opened []string
	options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state := newTestState(t, options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	canvas, texts := paintedTexts(state)
	if !contains(texts, "99.0.0") || !contains(texts, "Fixed") || !contains(texts, "the thing") {
		t.Fatalf("the dialog does not show the version and its notes: %q", texts)
	}
	// The notes are Markdown: a level-two heading is set larger than the body,
	// where plain lines would have painted both at the same size.
	heading, body := float32(0), float32(0)
	for _, text := range canvas.Texts() {
		switch text.Value {
		case "Fixed":
			heading = text.Style.Size
		case "the thing":
			body = text.Style.Size
		}
	}
	if heading <= body {
		t.Fatalf("the notes' heading (%.1f) is not larger than their body (%.1f): they are not Markdown", heading, body)
	}
	state.Click(hitUpdateDownload)
	if state.modal != modalNone || !reflect.DeepEqual(opened, []string{"https://example.com/release"}) {
		t.Fatalf("modal %d, opened %q", state.modal, opened)
	}
}

func TestALinkInTheReleaseNotesOpensInTheBrowser(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{
			Version: "99.0.0", URL: "https://example.com/release",
			Notes: "See [#12](https://example.com/12) for the details.",
		}, nil
	}
	var opened []string
	options.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	state := newTestState(t, options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	paintedTexts(state)
	state.Click(hitUpdateNotesLinks.At(0))
	if !reflect.DeepEqual(opened, []string{"https://example.com/12"}) {
		t.Fatalf("opened %q", opened)
	}
	if state.modal != modalUpdate {
		t.Fatalf("a link in the notes closed the dialog: modal %d", state.modal)
	}
}

func TestASkippedVersionStaysQuietAtStartupButNotWhenAsked(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	version := "99.0.0"
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{Version: version, URL: "https://example.com/r"}, nil
	}
	state := New(options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	state.Click(hitUpdateSkip)
	state.Close()

	again := New(options)
	again.update.firstDelay = time.Millisecond
	waitUpdate(t, again, func() bool { return again.update.available })
	if again.modal == modalUpdate || again.update.offer {
		t.Fatal("a skipped version was offered again at startup")
	}
	again.Click(hitSettingsUpdateCheck)
	waitUpdate(t, again, func() bool { return again.modal == modalUpdate })
	again.Close()

	version = "99.1.0"
	newer := New(options)
	newer.update.firstDelay = time.Millisecond
	defer newer.Close()
	waitUpdate(t, newer, func() bool { return newer.update.available })
	// A check nobody asked for announces the version without a dialog, which
	// would land on whatever the user is typing; the notice's button opens it.
	if newer.modal != modalNone || newer.update.offer {
		t.Fatal("a background check opened a dialog")
	}
	if len(newer.fileNotices.items) != 1 || newer.fileNotices.items[0].action != hitUpdateNoticeAction {
		t.Fatalf("the new version was not announced: %+v", newer.fileNotices.items)
	}
	newer.Click(hitUpdateNoticeAction)
	if newer.modal != modalUpdate || len(newer.fileNotices.items) != 0 {
		t.Fatalf("the notice's button did not open the offer and clear the notice: modal %d, notices %d", newer.modal, len(newer.fileNotices.items))
	}
}

func TestAnAvailableUpdateStaysInTheBarUntilThereIsNone(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{Version: "99.0.0", URL: "https://example.com/r"}, nil
	}
	state := newTestState(t, options)
	inBar := func() bool {
		var tools [maxBarTools]barTool
		for _, tool := range state.toolbarDockTools(state.defaultToolbarDockZone(toolbarWindow), &tools) {
			if tool.id == hitUpdateToolbar {
				return true
			}
		}
		return false
	}
	if inBar() {
		t.Fatal("an update button before any update is known")
	}
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return state.modal == modalUpdate })
	state.Click(hitUpdateLater)
	if !inBar() {
		t.Fatal("the bar lost the update button after Later")
	}
	state.Click(hitUpdateToolbar)
	if state.modal != modalUpdate {
		t.Fatalf("the bar button did not reopen the offer: modal %d", state.modal)
	}
	state.Click(hitUpdateSkip)
	if inBar() {
		t.Fatal("a skipped version still has a button in the bar")
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

func TestTheAutoCheckIsOnByDefaultRunsAndCanBeTurnedOff(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{Version: "99.0.0", URL: "https://example.com/r"}, nil
	}
	state := New(options)
	t.Cleanup(state.Close)
	if !state.updateAtStart {
		t.Fatal("the automatic check is not on by default")
	}
	state.update.firstDelay, state.update.every = time.Millisecond, time.Hour
	waitUpdate(t, state, func() bool { return state.update.available })
	if !strings.Contains(state.status, "99.0.0") {
		t.Fatalf("status %q does not announce the version", state.status)
	}
	state.Click(hitUpdateLater)
	state.Click(hitSettingsUpdateStartToggle)
	if state.update.timer != nil {
		t.Fatal("turning the check off left a timer armed")
	}
	state.Close()

	again := New(options)
	defer again.Close()
	if again.updateAtStart {
		t.Fatal("turning the check off was not saved")
	}
}

func TestTheAutoCheckAsksAgainAfterItsInterval(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	var calls atomic.Int32
	options.CheckUpdate = func() (update.Release, error) {
		calls.Add(1)
		return update.Release{Version: update.Current, URL: "https://example.com/r"}, nil
	}
	state := New(options)
	t.Cleanup(state.Close)
	state.update.firstDelay, state.update.every = time.Millisecond, 5*time.Millisecond
	waitUpdate(t, state, func() bool { return calls.Load() >= 2 })
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
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab}); !result.Repaint || len(world.configs) != 2 {
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

func TestASecondStartWithNothingToOpenBringsTheWindowForward(t *testing.T) {
	state, world, raised := launchState(t)
	state.Launch(nil)
	state.Wake()
	if *raised != 1 || len(world.configs) != 0 {
		t.Fatalf("raised %d times, dialed %d", *raised, len(world.configs))
	}
}

func TestAStartAtLoginDoesNotBringTheWindowForward(t *testing.T) {
	state, _, raised := launchState(t)
	state.Launch([]string{"--background"})
	state.Wake()
	if *raised != 0 {
		t.Fatalf("the window was raised %d times", *raised)
	}
}

func TestRelaunchChoiceOpensAWindowWithoutChangingOtherLaunches(t *testing.T) {
	options := freshOptions(t)
	options.Invalidate = func() {}
	opened, raised := 0, 0
	options.NewWindow = func() error { opened++; return nil }
	options.Raise = func() { raised++ }
	first := newTestState(t, options)
	openGeneralSettings(first)
	_, texts := paintedTexts(first)
	if !contains(texts, first.messages.Tools.GeneralRelaunch) {
		t.Fatal("the relaunch choice is missing from General settings")
	}
	first.Launch(nil)
	first.Wake()
	if raised != 1 || opened != 0 {
		t.Fatalf("default relaunch: raised %d, opened %d", raised, opened)
	}
	first.Click(hitSettingsRelaunchNewWindow)
	first.Launch(nil)
	first.Wake()
	if raised != 1 || opened != 1 {
		t.Fatalf("configured relaunch: raised %d, opened %d", raised, opened)
	}
	first.Launch([]string{"--background"})
	first.Wake()
	if raised != 1 || opened != 1 {
		t.Fatalf("background launch: raised %d, opened %d", raised, opened)
	}
	first.Launch([]string{"-unknown"})
	first.Wake()
	if opened != 1 {
		t.Fatal("a launch with arguments opened a new window")
	}
	first.Launch([]string{"ssh://bob@link.example.com/"})
	first.Wake()
	if first.modal != modalOpenLink || raised != 3 || opened != 1 {
		t.Fatalf("link launch: modal %v, raised %d, opened %d", first.modal, raised, opened)
	}
	first.Close()

	second := newTestState(t, options)
	second.Launch(nil)
	second.Wake()
	if opened != 2 {
		t.Fatal("the new-window choice did not survive a restart")
	}
	openGeneralSettings(second)
	second.Click(hitSettingsRelaunchRaise)
	second.Launch(nil)
	second.Wake()
	if opened != 2 || raised != 4 {
		t.Fatalf("restored default: raised %d, opened %d", raised, opened)
	}
}

func TestRelaunchUsesChoiceChangedInAnotherWindow(t *testing.T) {
	pair := newTwoWindows(t)
	opened := 0
	pair.second.options.NewWindow = func() error { opened++; return nil }
	openGeneralSettings(pair.first)
	pair.first.Click(hitSettingsRelaunchNewWindow)
	pair.second.Launch(nil)
	pair.second.Wake()
	if opened != 1 {
		t.Fatal("the last window did not pick up the relaunch choice from the other window")
	}
}

func TestThePreviousRunsCrashIsToldOnceAtStart(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.PreviousCrash = "/data/logs/crash-20261029-061442.log"
	state := newTestState(t, options)
	if state.modal != modalStateNotice {
		t.Fatalf("modal = %d, want the notice", state.modal)
	}
	state.buildStateNotice()
	if !strings.Contains(state.stateNotice.body, "crash-20261029-061442.log") {
		t.Fatalf("the notice does not name the report: %q", state.stateNotice.body)
	}
	if report := state.diagnosticReport(); !strings.Contains(report, "== Previous crash ==") {
		t.Fatalf("the diagnostics report leaves the crash out:\n%s", report)
	}
}

func TestWhatClosingDoesIsChosenInSettingsAndSurvivesARestart(t *testing.T) {
	options := freshOptions(t)
	options.StatusArea = func() bool { return true }
	first := newTestState(t, options)
	if !first.StaysInTray() {
		t.Fatal("closing quits by default; it has always stayed in the tray")
	}
	openGeneralSettings(first)
	_, texts := paintedTexts(first)
	if !contains(texts, first.messages.Tools.GeneralClose) {
		t.Fatalf("no close row: %q", texts)
	}
	first.Click(hitSettingsCloseQuit)
	if first.StaysInTray() {
		t.Fatal("choosing Quit still stays in the tray")
	}
	second := newTestState(t, options)
	if second.StaysInTray() {
		t.Fatal("the choice was forgotten by a restart")
	}
	second.Click(hitSettingsCloseTray)
	if !second.StaysInTray() {
		t.Fatal("choosing the tray again did not stick")
	}
}

func TestWithoutAStatusAreaThereIsNothingToChooseAboutClosing(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openGeneralSettings(state)
	_, texts := paintedTexts(state)
	if contains(texts, state.messages.Tools.GeneralClose) {
		t.Fatal("a close row on a desktop with no status area")
	}
}
