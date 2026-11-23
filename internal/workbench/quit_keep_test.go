package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestKeepWindowsPolicyStrings(t *testing.T) {
	for _, policy := range []keepWindowsPolicy{keepWindowsAlways, keepWindowsNever, keepWindowsAsk} {
		if got := keepWindowsPolicyFromString(policy.String()); got != policy {
			t.Fatalf("%q round trip = %v, want %v", policy, got, policy)
		}
	}
	if got := keepWindowsPolicyFromString(""); got != keepWindowsAlways {
		t.Fatalf("an unset policy = %v, want keep", got)
	}
}

func TestAskKeepWindowsOnExitAnswers(t *testing.T) {
	state := newTestState(t, testOptions(t))
	raised := 0
	state.options.Raise = func() { raised++ }
	state.keepWindows = keepWindowsAsk

	for _, test := range []struct {
		name string
		hit  gfx.ID
		want bool
	}{
		{name: "keep", hit: hitQuitKeepWindowsKeep, want: true},
		{name: "discard", hit: hitQuitKeepWindowsDiscard, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			answered, calls := false, 0
			if !state.AskKeepWindowsOnExit(func(keep bool) {
				calls++
				answered = keep
			}, nil) {
				t.Fatal("the ask policy did not open the question")
			}
			if state.modal != modalQuitKeepWindows {
				t.Fatalf("modal = %v, want the quit question", state.modal)
			}
			var canvas gfx.Canvas
			state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
			state.Click(test.hit)
			if calls != 1 {
				t.Fatalf("answer calls = %d, want 1", calls)
			}
			if answered != test.want {
				t.Fatalf("answer = %v, want %v", answered, test.want)
			}
			if state.modal != modalNone {
				t.Fatalf("modal after answering = %v, want none", state.modal)
			}
		})
	}
	if raised != 2 {
		t.Fatalf("raised the window %d times, want 2", raised)
	}
}

func TestAskKeepWindowsOnExitRequestsTheFrameThatShowsIt(t *testing.T) {
	options := testOptions(t)
	var calls []string
	options.Raise = func() { calls = append(calls, "raise") }
	options.Invalidate = func() { calls = append(calls, "invalidate") }
	state := newTestState(t, options)
	state.keepWindows = keepWindowsAsk
	calls = nil

	if !state.AskKeepWindowsOnExit(nil, nil) {
		t.Fatal("the ask policy did not open the question")
	}
	if len(calls) != 2 || calls[0] != "raise" || calls[1] != "invalidate" {
		t.Fatalf("opening the question did %v, want the window raised and then a repaint requested", calls)
	}

	state.CancelKeepWindowsOnExit()
	calls = nil
	state.keepWindows = keepWindowsAlways
	if state.AskKeepWindowsOnExit(nil, nil) {
		t.Fatal("a policy that does not ask opened the question")
	}
	if len(calls) != 0 {
		t.Fatalf("a policy that does not ask raised or repainted the window: %v", calls)
	}
}

func TestAskKeepWindowsOnExitCancels(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.keepWindows = keepWindowsAsk
	state.modal = modalRenameTab
	answered, cancelled := 0, 0
	if !state.AskKeepWindowsOnExit(func(bool) { answered++ }, func() { cancelled++ }) {
		t.Fatal("the ask policy did not open the question")
	}
	state.Click(hitQuitKeepWindowsCancel)
	if answered != 0 || cancelled != 1 {
		t.Fatalf("answered=%d cancelled=%d, want 0 and 1", answered, cancelled)
	}
	if state.modal != modalRenameTab {
		t.Fatalf("modal after cancel = %v, want the form the question replaced", state.modal)
	}
}

func TestAskKeepWindowsOnExitCancelKeepsTheFormUnderIt(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.keepWindows = keepWindowsAsk
	form := state.openForm(formDialog{title: "New host", submit: "Save"},
		formField{label: "Name"})
	form.set(0, "staging")
	if !state.AskKeepWindowsOnExit(nil, nil) {
		t.Fatal("the ask policy did not open the question")
	}
	if state.modal != modalQuitKeepWindows {
		t.Fatalf("modal = %v, want the quit question", state.modal)
	}
	state.Click(hitQuitKeepWindowsCancel)
	if state.modal != modalForm {
		t.Fatalf("modal after cancel = %v, want the form the question replaced", state.modal)
	}
	if state.form.title != "New host" || state.form.value(0) != "staging" {
		t.Fatalf("the form lost its contents: title %q, value %q", state.form.title, state.form.value(0))
	}
}

func TestKeepWindowsPolicyIsRestored(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.keepWindows = keepWindowsNever
	state.save()

	reopened := newTestState(t, options)
	if !reopened.DiscardsWindowsOnExit() {
		t.Fatal("the saved discard policy was not restored")
	}
}
