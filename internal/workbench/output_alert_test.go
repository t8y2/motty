package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
)

func TestBackgroundOutputTransitionsAndCommandPriority(t *testing.T) {
	w := liveAutofillSession(t)
	s := w.state
	session := &s.sessions[0]
	s.activeSection = sectionSettings
	w.shell.say("0 errors\r\n")
	s.Wake()
	if session.outputAlert.kind != outputAlertNone {
		t.Fatal("alerts must default off")
	}
	s.backgroundOutputAlerts = true
	w.shell.say("output\r\n")
	s.Wake()
	if session.outputAlert.kind != outputAlertRunning {
		t.Fatal("background output not noticed")
	}
	w.clock += outputQuietDelay
	s.Wake()
	if session.outputAlert.kind != outputAlertQuiet {
		t.Fatal("silence must become green")
	}
	token := session.term.folderToken
	marker := func(value string) string { return "\x1b]133;" + value + ";motty=" + token + "\a" }
	// A shell shows its prompt before the first command it reports.
	w.shell.say(marker("A") + marker("B"))
	s.Wake()
	// Completion and output in the same Drain must leave completion in control.
	w.shell.say(marker("C") + "failure\r\n" + marker("D;7"))
	s.Wake()
	if session.outputAlert.kind != outputAlertError {
		t.Fatal("nonzero status must win")
	}
	w.shell.say(marker("C") + "0 errors\r\n" + marker("D;0"))
	s.Wake()
	w.clock += 5 * time.Second
	s.Wake()
	if session.outputAlert.kind != outputAlertError {
		t.Fatal("success/output must not erase unread errors")
	}
	// An integrated command that goes quiet is still running until its D marker.
	s.activeSection = sectionTerminal
	s.refreshOutputAlerts(nil)
	s.activeSection = sectionSettings
	w.shell.say(marker("C") + "working")
	s.Wake()
	w.clock += 3 * time.Second
	s.Wake()
	if session.outputAlert.kind != outputAlertRunning {
		t.Fatal("integrated command ended by silence")
	}
	w.shell.say(marker("D;0"))
	s.Wake()
	if session.outputAlert.kind != outputAlertQuiet {
		t.Fatal("successful completion did not stop animation")
	}
	session.outputAlert.kind = outputAlertError
	// Disconnect must leave both the reminder and connection light semantics intact.
	session.phase = sshconn.PhaseClosed
	session.term.ended = true
	if kind, _ := s.sessionLight(session); kind != lightIdle {
		t.Fatal("connection light changed")
	}
	if session.outputAlert.kind != outputAlertError {
		t.Fatal("disconnect erased reminder")
	}
	s.activeSection = sectionTerminal
	s.refreshOutputAlerts(nil)
	if session.outputAlert.kind != outputAlertNone {
		t.Fatal("viewing must acknowledge")
	}
	w.shell.say("visible output")
	s.Wake()
	if session.outputAlert.kind != outputAlertNone {
		t.Fatal("visible output must not alert")
	}
}

func TestBackgroundOutputSplitVisibilityAndAggregation(t *testing.T) {
	s := threeVisiblePaneState(t)
	s.backgroundOutputAlerts = true
	workspace := s.deck.ActiveWorkspace()
	for i := range s.sessions {
		session := &s.sessions[i]
		s.receiveBackgroundOutput(session)
		if session.outputAlert.kind != outputAlertNone {
			t.Fatal("visible unfocused split pane alerted")
		}
	}
	s.activeSection = sectionSettings
	s.sessions[0].outputAlert.kind = outputAlertQuiet
	s.sessions[1].outputAlert.kind = outputAlertRunning
	s.sessions[2].outputAlert.kind = outputAlertError
	if s.workspaceOutputAlert(workspace) != outputAlertError {
		t.Fatal("workspace did not aggregate errors first")
	}
	s.sessions[2].outputAlert.kind = outputAlertNone
	if s.workspaceOutputAlert(workspace) != outputAlertRunning {
		t.Fatal("running should outrank quiet")
	}
	s.activeSection = sectionTerminal
	s.refreshOutputAlerts(nil)
	for i := range s.sessions {
		if s.sessions[i].outputAlert.kind != outputAlertNone {
			t.Fatal("viewing splits must clear all visible panes")
		}
	}
	s.winChrome.minimized = true
	s.receiveBackgroundOutput(&s.sessions[1])
	if s.sessions[1].outputAlert.kind != outputAlertRunning {
		t.Fatal("minimized pane counts as background")
	}
}

func TestBackgroundOutputSettingPersistsButNotReminders(t *testing.T) {
	s := newTestState(t, testOptions(t))
	s.Click(hitSettings)
	s.Click(hitSettingsOutputAlerts)
	s.activeSection = sectionHosts
	openHost(s)
	s.sessions[0].outputAlert.kind = outputAlertError
	s.save()
	restored := newTestState(t, s.options)
	if !restored.backgroundOutputAlerts {
		t.Fatal("setting not restored")
	}
	for i := range restored.sessions {
		if restored.sessions[i].outputAlert.kind != outputAlertNone {
			t.Fatal("reminder was persisted")
		}
	}
	s.Click(hitSettings)
	s.Click(hitSettingsOutputAlerts)
	if s.backgroundOutputAlerts || s.sessions[0].outputAlert.kind != outputAlertNone {
		t.Fatal("disabling must clear reminders")
	}
}

func TestBackgroundOutputTitlesRespectMotionAndOrientation(t *testing.T) {
	s := threeVisiblePaneState(t)
	s.backgroundOutputAlerts = true
	// Settings hide terminals, but the workspace tabs remain available.
	s.activeSection = sectionSettings
	s.sessions[0].outputAlert.kind = outputAlertError
	for _, orientation := range []tabOrientation{tabsHorizontal, tabsVertical} {
		s.setTabOrientation(orientation)
		for _, reduced := range []bool{false, true} {
			var canvas gfx.Canvas
			canvas.SetReduceMotion(reduced)
			s.Paint(&canvas, gfx.Size{Width: 1400, Height: 1100})
			found := false
			for _, text := range canvas.Texts() {
				if text.Style.Color.Red == s.pal.warning.Red && text.Style.Color.Green == s.pal.warning.Green {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing yellow title: orientation %v reduced %v", orientation, reduced)
			}
		}
	}
}

func TestOutputAlertAnimationOnlyRequestsFramesForVisibleDynamicTitles(t *testing.T) {
	s := threeVisiblePaneState(t)
	s.backgroundOutputAlerts = true
	s.activeSection = sectionSettings
	s.sessions[0].outputAlert.kind = outputAlertError
	var layout gfx.Canvas
	s.Paint(&layout, gfx.Size{Width: 1400, Height: 900})
	tab := s.chrome.bar.tabs[0]
	strip := gfx.Rect{Width: 1400, Height: 900}
	for _, kind := range []outputAlertKind{outputAlertRunning, outputAlertError, outputAlertQuiet} {
		tab.output = kind
		for _, reduced := range []bool{false, true} {
			var canvas gfx.Canvas
			canvas.SetReduceMotion(reduced)
			canvas.SetClock(func() time.Duration { return 900 * time.Millisecond })
			canvas.BeginFrame()
			s.paintOutputAlertTitle(&canvas, &tab, strip)
			canvas.EndFrame()
			_, scheduled := canvas.NextFrame()
			if scheduled != (!reduced && kind != outputAlertQuiet) {
				t.Fatalf("kind %v reduced %v scheduled %v", kind, reduced, scheduled)
			}
			if kind == outputAlertRunning && !reduced {
				texts := canvas.Texts()
				if len(texts) < 2 {
					t.Fatal("wave did not paint color bands")
				}
				different := false
				for _, text := range texts {
					if text.Style.Color != texts[0].Style.Color {
						different = true
					}
					if text.Value != texts[0].Value || text.Bounds != texts[0].Bounds {
						t.Fatal("wave moved text")
					}
				}
				if !different {
					t.Fatal("wave has no highlight")
				}
				// The wave rises and falls, so it has at most two runs per color.
				if len(texts) > 2*sweepBands+1 {
					t.Fatalf("the wave painted %d layers, want at most %d cached colors", len(texts), 2*sweepBands+1)
				}
			}
		}
	}
	var hidden gfx.Canvas
	tab.output = outputAlertError
	s.paintOutputAlertTitle(&hidden, &tab, gfx.Rect{X: 2000, Y: 2000, Width: 10, Height: 10})
	if _, scheduled := hidden.NextFrame(); scheduled {
		t.Fatal("offscreen title kept requesting frames")
	}
}

// The integration reports a command start while it installs itself, before the
// shell has shown a prompt, and the first prompt reports whatever status the
// shell inherited. Counting that pair as a command paints an error on a session
// that connected in the background and ran nothing.
func TestTheStatusOfTheFirstPromptIsNotACommandCompletion(t *testing.T) {
	w := liveAutofillSession(t)
	s, session := w.state, &w.state.sessions[0]
	s.activeSection = sectionSettings
	s.backgroundOutputAlerts = true
	token := session.term.folderToken
	marker := func(value string) string { return "\x1b]133;" + value + ";motty=" + token + "\a" }
	// A fresh shell: the fixture has already shown a prompt, a background
	// connection starts one.
	s.configureTerminal(session)
	w.shell.say(marker("C"))
	w.shell.say(marker("A"))
	w.shell.say(marker("D;7"))
	s.Wake()
	if kind := session.outputAlert.kind; kind == outputAlertError {
		t.Fatal("the inherited status of the first prompt painted an error")
	}
	// A command run after that prompt still reports its own status.
	w.shell.say(marker("C") + "done\r\n" + marker("D;1"))
	s.Wake()
	if kind := session.outputAlert.kind; kind != outputAlertError {
		t.Fatalf("a failed command after the prompt left alert %v", kind)
	}
}
