//go:build darwin || windows || linux

package main

import (
	"testing"
)

// fakeQuitWindow stands in for a workbench window in the quit-path tests. Only
// the parts the quit paths touch are real; the rest of a window is not needed
// to decide whether its record survives.
type fakeQuitWindow struct {
	visible  bool
	asks     bool
	keeps    bool
	discards bool
	// documents says the window still has unsaved documents. autoConfirm closes
	// them at once; otherwise the close stays pending, as when the user is still
	// answering the document dialog.
	documents   bool
	autoConfirm bool
	// onAsk runs when the question opens; nil leaves it open for the test to
	// answer or cancel through the window.
	onAsk func(answer func(keep bool), cancelled func())

	openQuestion        bool
	answer              func(bool)
	cancel              func()
	questionOpens       int
	documentConfirms    int
	cancelQuestionCalls int
	order               *[]string
}

func (w *fakeQuitWindow) Launch([]string) {}

func (w *fakeQuitWindow) ConfirmDocumentClose(allow func()) bool {
	w.documentConfirms++
	if w.order != nil {
		*w.order = append(*w.order, "documents")
	}
	if !w.documents {
		return false
	}
	if w.autoConfirm {
		// Confirming the dialog closes the document, so a resumed pass through
		// this window does not ask again.
		w.documents = false
		allow()
	}
	return true
}

func (w *fakeQuitWindow) WindowVisible() bool          { return w.visible }
func (w *fakeQuitWindow) AsksAboutWindowsOnExit() bool { return w.asks }
func (w *fakeQuitWindow) KeepsWindowsOnExit() bool     { return w.keeps }
func (w *fakeQuitWindow) DiscardsWindowsOnExit() bool  { return w.discards }

func (w *fakeQuitWindow) AskKeepWindowsOnExit(answer func(keep bool), cancelled func()) bool {
	if !w.asks {
		return false
	}
	w.questionOpens++
	w.openQuestion = true
	w.answer, w.cancel = answer, cancelled
	if w.order != nil {
		*w.order = append(*w.order, "ask")
	}
	if w.onAsk != nil {
		w.onAsk(answer, cancelled)
	}
	return true
}

func (w *fakeQuitWindow) CancelKeepWindowsOnExit() bool {
	w.cancelQuestionCalls++
	if !w.openQuestion {
		return false
	}
	w.openQuestion = false
	if w.cancel != nil {
		w.cancel()
	}
	return true
}

func newQuitSet(state *fakeQuitWindow) *windowSet {
	return &windowSet{states: []quitWindow{state}}
}

// The program-wide quit paths must all read the same three policies the same
// way: keep, drop, or ask.
func TestQuitPathsFollowTheKeepWindowsPolicy(t *testing.T) {
	allowCalls := func(allow *int) func() { return func() { (*allow)++ } }

	for _, policy := range []struct {
		name     string
		window   fakeQuitWindow
		answer   bool
		wantKeep bool
	}{
		{name: "keep", window: fakeQuitWindow{visible: true, keeps: true}, wantKeep: true},
		{name: "drop", window: fakeQuitWindow{visible: true, discards: true}, wantKeep: false},
		{name: "ask-keep", window: fakeQuitWindow{visible: true, asks: true, onAsk: func(answer func(bool), _ func()) { answer(true) }}, wantKeep: true},
		{name: "ask-drop", window: fakeQuitWindow{visible: true, asks: true, onAsk: func(answer func(bool), _ func()) { answer(false) }}, wantKeep: false},
	} {
		t.Run(policy.name, func(t *testing.T) {
			for _, entry := range []struct {
				name string
				run  func(*windowSet, *fakeQuitWindow, func())
			}{
				{name: "quit-command", run: func(set *windowSet, window *fakeQuitWindow, allow func()) { set.quitWithDocuments(allow) }},
				{name: "last-window-close", run: func(set *windowSet, window *fakeQuitWindow, allow func()) { set.closeLastWindow(window, allow) }},
			} {
				t.Run(entry.name, func(t *testing.T) {
					window := policy.window
					set := newQuitSet(&window)
					allowed := 0
					entry.run(set, &window, allowCalls(&allowed))
					if allowed != 1 || !set.isQuitting() {
						t.Fatalf("allowed=%d quitting=%v, want 1 and true", allowed, set.isQuitting())
					}
					if got := set.keepsWindowsOnQuit(); got != policy.wantKeep {
						t.Fatalf("keeps windows = %v, want %v", got, policy.wantKeep)
					}
					if policy.window.asks && window.questionOpens != 1 {
						t.Fatalf("the question opened %d times, want 1", window.questionOpens)
					}
					if !policy.window.asks && window.questionOpens != 0 {
						t.Fatalf("the question opened %d times for a setting that does not ask", window.questionOpens)
					}
				})
			}
		})
	}
}

// Closing a window decides its own record: a quit command overrides the
// setting, another open window drops the record, and only the last window under
// a keep-or-ask setting leaves it behind.
func TestWindowCloseKeepsItsRecordOnlyWhenTheSettingSaysSo(t *testing.T) {
	for _, test := range []struct {
		name       string
		quitting   bool
		keepOnQuit bool
		remaining  int
		discards   bool
		want       bool
	}{
		{name: "quit-keeps", quitting: true, keepOnQuit: true, remaining: 0, want: true},
		{name: "quit-drops", quitting: true, keepOnQuit: false, remaining: 0, want: false},
		{name: "another-window-open", quitting: false, remaining: 1, want: false},
		{name: "last-window-drop-policy", quitting: false, remaining: 0, discards: true, want: false},
		{name: "last-window-keep-policy", quitting: false, remaining: 0, want: true},
		{name: "last-window-ask-policy", quitting: false, remaining: 0, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := keepWindowRecordOnClose(test.quitting, test.keepOnQuit, test.remaining, test.discards); got != test.want {
				t.Fatalf("keep record = %v, want %v", got, test.want)
			}
		})
	}
}

// A second quit request while the question is open leaves the open question
// alone instead of opening another one or quitting past it.
func TestASecondQuitDoesNotReplaceTheOpenQuestion(t *testing.T) {
	window := &fakeQuitWindow{visible: true, asks: true}
	set := newQuitSet(window)
	allowed := 0
	set.quitWithDocuments(func() { allowed++ })
	set.quitWithDocuments(func() { allowed++ })
	if window.questionOpens != 1 {
		t.Fatalf("the question opened %d times, want 1", window.questionOpens)
	}
	if allowed != 0 {
		t.Fatalf("allowed=%d while the question was open, want 0", allowed)
	}
	if !set.askingKeepWindows {
		t.Fatal("the guard was not set while the question was open")
	}
}

// A window that closes or hides while holding the question releases the guard,
// so the next quit asks again instead of being swallowed forever.
func TestClosingTheAskingWindowReleasesTheQuestion(t *testing.T) {
	window := &fakeQuitWindow{visible: true, asks: true}
	set := newQuitSet(window)
	allowed := 0
	set.quitWithDocuments(func() { allowed++ })
	set.cancelKeepWindows(window)
	if set.askingKeepWindows {
		t.Fatal("the guard stayed set after the asking window went away")
	}
	if window.cancelQuestionCalls != 1 {
		t.Fatalf("the question was cancelled %d times, want 1", window.cancelQuestionCalls)
	}
	set.quitWithDocuments(func() { allowed++ })
	if window.questionOpens != 2 {
		t.Fatalf("the question opened %d times after the release, want 2", window.questionOpens)
	}
	if allowed != 0 {
		t.Fatalf("allowed=%d, want 0", allowed)
	}
}

// A window that is not holding the question must not release it.
func TestClosingAnotherWindowKeepsTheQuestion(t *testing.T) {
	asking := &fakeQuitWindow{visible: true, asks: true}
	other := &fakeQuitWindow{}
	set := &windowSet{states: []quitWindow{asking, other}}
	set.quitWithDocuments(func() {})
	set.cancelKeepWindows(other)
	if !set.askingKeepWindows {
		t.Fatal("a different window's close released the open question")
	}
}

// The keep question comes before the unsaved-document dialog, so cancelling the
// question aborts the whole quit without closing any document.
func TestKeepQuestionComesBeforeUnsavedDocuments(t *testing.T) {
	var order []string
	window := &fakeQuitWindow{
		visible: true, asks: true, documents: true,
		onAsk: func(_ func(bool), cancelled func()) { cancelled() },
		order: &order,
	}
	set := newQuitSet(window)
	allowed := 0
	set.quitWithDocuments(func() { allowed++ })
	if len(order) != 1 || order[0] != "ask" {
		t.Fatalf("order = %v, want only the question; a cancelled question must not touch documents", order)
	}
	if allowed != 0 || set.isQuitting() {
		t.Fatalf("allowed=%d quitting=%v, want 0 and false", allowed, set.isQuitting())
	}

	// Answering continues to the documents, still in that order.
	order = nil
	window = &fakeQuitWindow{
		visible: true, asks: true, documents: true, autoConfirm: true,
		onAsk: func(answer func(bool), _ func()) { answer(true) },
		order: &order,
	}
	set = newQuitSet(window)
	allowed = 0
	set.quitWithDocuments(func() { allowed++ })
	if len(order) < 2 || order[0] != "ask" || order[1] != "documents" {
		t.Fatalf("order = %v, want the question before the documents", order)
	}
	if allowed != 1 || !set.keepsWindowsOnQuit() {
		t.Fatalf("allowed=%d keeps=%v, want 1 and true", allowed, set.keepsWindowsOnQuit())
	}
}

// A document the user keeps open after answering the keep question cancels the
// whole quit; the keep choice does not take effect on its own.
func TestCancelledDocumentCloseAbortsTheQuit(t *testing.T) {
	window := &fakeQuitWindow{
		visible: true, asks: true, documents: true,
		onAsk: func(answer func(bool), _ func()) { answer(false) },
	}
	set := newQuitSet(window)
	allowed := 0
	set.quitWithDocuments(func() { allowed++ })
	if allowed != 0 || set.isQuitting() {
		t.Fatalf("allowed=%d quitting=%v, want 0 and false", allowed, set.isQuitting())
	}
}
