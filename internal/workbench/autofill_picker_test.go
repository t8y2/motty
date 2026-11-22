package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/autofill"
)

// savedPasswordWorld is a live session with the terminal in front again, the
// way it is after Settings was used to save a password.
func savedPasswordWorld(t *testing.T) *autofillWorld {
	t.Helper()
	w := liveAutofillSession(t)
	addCredential(t, w.state, "root@cloudpivot", "root-secret")
	addCredential(t, w.state, "deploy", "deploy-secret")
	w.state.activeSection = sectionTerminal
	return w
}

func TestAPasswordPromptOffersSavedPasswordsWithoutARule(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) == 0 || w.state.modal != modalNone {
		t.Fatalf("the prompt did not offer saved passwords: pick %+v, modal %d", w.state.autofillRules.pick, w.state.modal)
	}
	if got := w.shell.typed(); got != "" {
		t.Fatalf("the offer answered by itself: %q", got)
	}

	// Two Down presses choose the second saved password; Enter sends it.
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("typed %q, want the chosen password", got)
	}
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("the offer stayed open after a password was sent")
	}
}

func TestTheHostPasswordIsOfferedFirst(t *testing.T) {
	w := savedPasswordWorld(t)
	session := &w.state.sessions[0]
	if err := w.state.secrets.vault.Set(w.state.passwordName(session), "host-secret"); err != nil {
		t.Fatal(err)
	}

	w.output("Password: ")
	pick := w.state.autofillRules.pick
	if len(pick.candidates) < 2 || !pick.candidates[0].host || pick.candidates[0].name != session.host.Label {
		t.Fatalf("candidates = %+v", pick.candidates)
	}
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "host-secret\r" {
		t.Fatalf("typed %q", got)
	}
}

func TestThePasswordOfferSendsNothingWhenClosed(t *testing.T) {
	for _, prompt := range []string{"Password: ", "[sudo] password for dev: ", "deploy@server-b's password: "} {
		t.Run(prompt, func(t *testing.T) {
			w := savedPasswordWorld(t)
			w.output(prompt)
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEscape})
			if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
				t.Fatalf("Escape sent something: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
			}

			// A style change must not reopen the dismissed prompt.
			w.output("\x1b[0m")
			if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
				t.Fatalf("the offer reopened on the same prompt: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
			}
		})
	}
}

func TestTypingAtAPasswordPromptTakesTheOfferBack(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyUnknown, Text: "r", Typed: "r"})
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("typing kept the offer open")
	}
	if got := w.shell.typed(); got != "r" {
		t.Fatalf("typed %q, want the typed character to reach the shell", got)
	}
}

func TestSuppliedTextTakesTheOfferBack(t *testing.T) {
	for _, event := range []app.TextEvent{
		{Kind: app.TextInsert, Text: "by-hand"},
		{Kind: app.TextPaste, Text: "by-hand"},
	} {
		w := savedPasswordWorld(t)
		w.output("Password: ")
		w.state.terminalText(w.state.sessions[0].ID, event)
		if len(w.state.autofillRules.pick.candidates) != 0 {
			t.Fatalf("%v kept the offer open", event.Kind)
		}
		if got := w.shell.typed(); got != "by-hand" {
			t.Fatalf("%v typed %q, want the supplied text to reach the shell", event.Kind, got)
		}
	}
}

func TestThePasswordOfferWaitsForTheTerminalTheUserIsLookingAt(t *testing.T) {
	w := liveAutofillSession(t)
	addCredential(t, w.state, "root", "root-secret")
	w.state.activeSection = sectionSettings
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("an offer opened behind Settings: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestClickingASavedPasswordSendsIt(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("Password: ")
	w.state.Click(hitAutofillPickItem.At(1))
	if got := w.shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("typed %q", got)
	}
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("the offer stayed open after a click")
	}
}

func TestThePasswordOfferStaysOffWithTheAssistSwitch(t *testing.T) {
	for _, prompt := range []string{"Password: ", "[sudo] password for dev: ", "deploy@server-b's password: "} {
		t.Run(prompt, func(t *testing.T) {
			w := savedPasswordWorld(t)
			w.state.setPasswordHint(false)
			w.output(prompt)
			if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
				t.Fatalf("the offer ignored the switch: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
			}
		})
	}
}

func TestNoSavedPasswordMeansNoOffer(t *testing.T) {
	w := liveAutofillSession(t)
	w.output("Password: ")
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "" {
		t.Fatalf("an empty offer opened: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestAConfiguredRuleStillAnswersBeforeTheOffer(t *testing.T) {
	w := liveAutofillSession(t)
	addCredential(t, w.state, "root", "root-secret")
	w.addRule(t, "token", `^Password:`, "", autofill.SendText, "rule-secret")
	w.state.Click(hitAutofillToggleList.At(0))
	w.state.activeSection = sectionTerminal

	w.output("Password: ")
	if w.state.modal != modalAutofillConfirm || len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatalf("the rule did not answer first: modal %d, pick %+v", w.state.modal, w.state.autofillRules.pick)
	}
}

func TestSudoAndNestedSSHCommandsOfferSavedIdentities(t *testing.T) {
	for _, output := range []string{
		"$ sudo ls\r\n[sudo] password for dev: ",
		"$ sudo -i\r\n[sudo: authenticate] Password: ",
		"$ su -\r\nPassword: ",
		"$ ssh deploy@server-b\r\ndeploy@server-b's password: ",
		"$ scp file deploy@server-b:/tmp/\r\ndeploy@server-b's password: ",
	} {
		t.Run(output, func(t *testing.T) {
			w := savedPasswordWorld(t)
			w.output(output)
			if len(w.state.autofillRules.pick.candidates) != 2 || w.shell.typed() != "" {
				t.Fatalf("pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
			}
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			if w.shell.typed() != "" {
				t.Fatal("moving the selection sent a password")
			}
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
			if got := w.shell.typed(); got != "deploy-secret\r" {
				t.Fatalf("typed %q", got)
			}
		})
	}
}

func TestPasswordOfferFiltersKnownUsersAndShowsIdentityUsernames(t *testing.T) {
	w := savedPasswordWorld(t)
	state := w.state
	state.credentials[0].User = "root"
	state.credentials[1].User = "deploy"
	addCredential(t, state, "password-only", "fallback-secret")
	state.activeSection = sectionTerminal
	session := &state.sessions[0]
	session.endpoint.User = "outer-user"
	if err := state.secrets.vault.Set(state.passwordName(session), "outer-secret"); err != nil {
		t.Fatal(err)
	}

	for _, prompt := range []string{"deploy@server-b's password: ", "[sudo] password for deploy: "} {
		w.output("\r\n" + prompt)
		candidates := state.autofillRules.pick.candidates
		if len(candidates) != 2 || candidates[0].credentialID != state.credentials[1].ID ||
			candidates[0].detail != "deploy" || candidates[1].name != "password-only" {
			t.Fatalf("%q candidates = %+v", prompt, candidates)
		}
		key(state, 0, gfx.KeyEvent{Key: gfx.KeyEscape})
	}
	// A bare prompt cannot tell us which account su is switching to.
	w.output("\r\nPassword: ")
	if got := len(state.autofillRules.pick.candidates); got != 4 {
		t.Fatalf("bare prompt offered %d passwords, want the host and all identities", got)
	}
}

func TestSharedHostIdentityIsListedOnceWithTheEffectiveLogin(t *testing.T) {
	w := savedPasswordWorld(t)
	state := w.state
	session := &state.sessions[0]
	session.host.Credential = state.credentials[0].ID
	session.endpoint.User = "dev"
	state.credentials[0].User = "root" // the host's explicit user overrides it
	w.output("[sudo] password for dev: ")
	candidates := state.autofillRules.pick.candidates
	if len(candidates) != 2 || !candidates[0].host || candidates[0].detail != "dev" {
		t.Fatalf("host identity duplicated or mislabeled: %+v", candidates)
	}
}

func TestLocalTerminalCanOfferIdentitiesForNestedSSH(t *testing.T) {
	w := savedPasswordWorld(t)
	session := &w.state.sessions[0]
	session.HostID = localHostID
	session.endpoint.Host = ""
	w.output("deploy@server-b's password: ")
	w.state.Click(hitAutofillPickItem.At(1))
	if got := w.shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("local terminal typed %q", got)
	}
}

func TestPasswordOfferDoesNotAppendToAnUnsubmittedManualPassword(t *testing.T) {
	w := savedPasswordWorld(t)
	w.output("deploy@server-b's pass")
	w.state.terminalText(w.state.sessions[0].ID, app.TextEvent{Kind: app.TextInsert, Text: "by-hand"})
	w.output("word: ") // the password was not echoed
	if len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "by-hand" {
		t.Fatalf("offer reopened over manual input: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestPasswordOfferRechecksQueuedOutputBeforeConfirming(t *testing.T) {
	for _, confirm := range []string{"keyboard", "mouse"} {
		t.Run(confirm, func(t *testing.T) {
			w := savedPasswordWorld(t)
			w.output("deploy@server-b's password: ")
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			w.shell.say("\r\nConnection closed\r\n$ ") // received, but not pumped yet
			if confirm == "keyboard" {
				key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
			} else {
				w.state.Click(hitAutofillPickItem.At(0))
			}
			if got := w.shell.typed(); got != "" {
				t.Fatalf("answered a stale prompt: %q", got)
			}
		})
	}
}

func TestPasswordOfferCannotAnswerBehindADialogOrInABackgroundTab(t *testing.T) {
	for _, change := range []string{"dialog", "tab", "section", "alternate-screen"} {
		t.Run(change, func(t *testing.T) {
			w := savedPasswordWorld(t)
			w.output("deploy@server-b's password: ")
			switch change {
			case "dialog":
				w.state.modal = modalPassword
			case "tab":
				w.state.activeSessionID++
			case "section":
				w.state.activeSection = sectionSettings
			case "alternate-screen":
				w.output("\x1b[?1049hPassword: ")
			}
			w.state.Click(hitAutofillPickItem.At(0))
			if got := w.shell.typed(); got != "" {
				t.Fatalf("answered a hidden prompt: %q", got)
			}
		})
	}
}

func TestChosenPasswordStaysOutOfTerminalRecordingAndBroadcast(t *testing.T) {
	state, workspace, sessions := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	addCredential(t, state, "deploy", "deploy-secret")
	state.activeSection = sectionTerminal
	state.activeSessionID = sessions[0].ID
	state.broadcast = workspace
	session, _ := state.sessionByIDPointer(sessions[0].ID)
	peer, _ := state.sessionByIDPointer(sessions[1].ID)
	state.toggleTerminalRecording(session)
	shell := session.term.shell.(*fakeShell)
	shell.say("deploy@server-b's password: ")
	state.Wake()
	session.term.autofill.Retry(state.now())
	state.scanAutofill(session)
	state.Click(hitAutofillPickItem.At(0))
	if got := shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("typed %q", got)
	}
	if got := peer.term.shell.(*fakeShell).typed(); got != "" {
		t.Fatalf("password was broadcast to a peer: %q", got)
	}
	for _, event := range session.term.recording.events {
		if event.direction == terminalRecordingInput || strings.Contains(string(event.data), "deploy-secret") {
			t.Fatal("the selected password entered the recording")
		}
	}
}

func TestPasswordPickerReopensForANewAuthenticationAttempt(t *testing.T) {
	for _, prompt := range []string{"Password: ", "[sudo: authenticate] Password: ", "deploy@server-b's password: "} {
		t.Run(prompt, func(t *testing.T) {
			w := savedPasswordWorld(t)
			w.output(prompt)
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
			w.output("\x1b[0m")
			if len(w.state.autofillRules.pick.candidates) != 0 {
				t.Fatal("styling the answered prompt reopened it")
			}
			w.output("\r\nPermission denied, try again.\r\n" + prompt)
			pick := &w.state.autofillRules.pick
			if len(pick.candidates) != 2 || pick.selected != -1 || w.shell.typed() != "root-secret\r" {
				t.Fatalf("retry pick %+v, typed %q", pick, w.shell.typed())
			}
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
			key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
			if got := w.shell.typed(); got != "root-secret\rdeploy-secret\r" {
				t.Fatalf("retry typed %q", got)
			}
		})
	}
}

func TestWrappedSSHPasswordPromptKeepsItsUserAndConfirms(t *testing.T) {
	w := savedPasswordWorld(t)
	w.state.credentials[0].User = "root"
	w.state.credentials[1].User = "deploy"
	w.output("\r\n")
	screen := w.state.sessions[0].term.screen
	cols, _ := screen.Size()
	w.output("deploy@" + strings.Repeat("node.", cols/5) + "example.com's password: ")
	if !screen.Row(screen.Cursor().Y - 1).Wrapped {
		t.Fatal("fixture did not wrap")
	}
	candidates := w.state.autofillRules.pick.candidates
	if len(candidates) != 1 || candidates[0].credentialID != w.state.credentials[1].ID {
		t.Fatalf("wrapped prompt candidates = %+v", candidates)
	}
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "deploy-secret\r" {
		t.Fatalf("wrapped prompt typed %q", got)
	}
}

func TestReturningFromSettingsResumesThePasswordOffer(t *testing.T) {
	w := savedPasswordWorld(t)
	w.state.Click(hitSettings)
	w.output("[sudo: authenticate] Password: ")
	if len(w.state.autofillRules.pick.candidates) != 0 {
		t.Fatal("list opened behind Settings")
	}
	if !w.state.activateTab(0) {
		t.Fatal("could not return to the terminal")
	}
	w.state.Wake()
	if len(w.state.autofillRules.pick.candidates) != 2 || w.shell.typed() != "" {
		t.Fatalf("resumed pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestPasswordOffersFollowTwoWaitingTerminalPanes(t *testing.T) {
	state, _, sessions := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	addCredential(t, state, "deploy", "deploy-secret")
	for _, id := range []uint32{sessions[0].ID, sessions[1].ID} {
		state.ActivatePane(id)
		session, _ := state.sessionByIDPointer(id)
		session.term.shell.(*fakeShell).say("[sudo: authenticate] Password: ")
		state.Wake()
		session.term.autofill.Retry(state.now())
		state.Wake()
		if state.autofillRules.pick.sessionID != id {
			t.Fatalf("prompt belongs to session %d, want %d", state.autofillRules.pick.sessionID, id)
		}
	}
	state.ActivatePane(sessions[0].ID)
	state.Wake()
	if state.autofillRules.pick.sessionID != sessions[0].ID {
		t.Fatal("the first pane lost its unanswered password offer")
	}
}

func TestFindFieldKeysCannotSelectOrConfirmAPassword(t *testing.T) {
	w := savedPasswordWorld(t)
	openFindForTest(t, w.state)
	w.output("[sudo: authenticate] Password: ")
	w.state.Key(gfx.KeyEvent{Target: hitTerminalFindField, Key: gfx.KeyDown})
	w.state.Key(gfx.KeyEvent{Target: hitTerminalFindField, Key: gfx.KeyEnter})
	if w.shell.typed() != "" || w.state.autofillRules.pick.selected != -1 {
		t.Fatalf("find keys selected a password: pick %+v, typed %q", w.state.autofillRules.pick, w.shell.typed())
	}
	// Returning to the grid still lets the user make an explicit selection.
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "root-secret\r" {
		t.Fatalf("terminal confirmation typed %q", got)
	}
}

func TestPasswordOfferCannotConfirmAnIdenticalPromptOnANewRow(t *testing.T) {
	w := savedPasswordWorld(t)
	prompt := "deploy@server-b's password: "
	w.output(prompt)
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyDown})
	w.shell.say("\r\n" + prompt)
	key(w.state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	if got := w.shell.typed(); got != "" {
		t.Fatalf("confirmed an obsolete prompt: %q", got)
	}
}

func TestPasswordPickerDoesNotBypassACustomRulesRepeatProtection(t *testing.T) {
	w := savedPasswordWorld(t)
	w.addRule(t, "password", `^Password:`, "", autofill.SendText, "rule-secret")
	w.state.Click(hitAutofillToggleList.At(0))
	w.state.activeSection = sectionTerminal
	w.output("Password: ")
	w.state.Click(hitAutofillAllow)
	w.output("\r\nDenied.\r\nPassword: ")
	if w.state.modal != modalNone || len(w.state.autofillRules.pick.candidates) != 0 || w.shell.typed() != "rule-secret\r" {
		t.Fatalf("custom rule protection bypassed: modal %d, pick %+v, typed %q", w.state.modal, w.state.autofillRules.pick, w.shell.typed())
	}
}

func TestDeferredPasswordOfferSchedulesOnlyAVisibleLiveTerminal(t *testing.T) {
	w := savedPasswordWorld(t)
	w.state.autofillRules.armed = true // configured rules must not force idle redraws
	session := &w.state.sessions[0]
	for _, blocked := range []string{"background", "dialog", "ended", "no-shell", "ready"} {
		t.Run(blocked, func(t *testing.T) {
			w.state.activeSection = sectionTerminal
			w.state.modal = modalNone
			session.term.ended = false
			session.term.shell = w.shell
			switch blocked {
			case "background":
				w.state.activeSection = sectionSettings
			case "dialog":
				w.state.modal = modalPassword
			case "ended":
				session.term.ended = true
			case "no-shell":
				session.term.shell = nil
			}
			session.term.autofill.Retry(w.state.now())
			var canvas gfx.Canvas
			w.state.requestAutofillFrames(&canvas)
			_, scheduled := canvas.NextFrame()
			if scheduled != (blocked == "ready") {
				t.Fatalf("deferred prompt scheduled a frame: %v", scheduled)
			}
		})
	}
}
