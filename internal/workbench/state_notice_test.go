package workbench

import (
	"path/filepath"
	"testing"
)

func TestTheCrashNoticeOffersTheReportInTheFileManager(t *testing.T) {
	options := freshOptions(t)
	report := filepath.Join(t.TempDir(), "logs", "crash-20261029-061442.log")
	options.PreviousCrash = report
	opened := make(chan string, 1)
	options.RevealPath = func(path string) error {
		opened <- path
		return nil
	}
	state := newTestState(t, options)
	canvas, _ := paintedTexts(state)
	if !hasHit(canvas, hitStateNoticeReveal) {
		t.Fatalf("the crash notice has no reveal action: %+v", canvas.Hits())
	}
	state.Click(hitStateNoticeReveal)
	waitFor(t, state, func() bool { return len(opened) == 1 })
	if got := <-opened; got != report {
		t.Fatalf("opened %q, want the crash report %q", got, report)
	}
	if state.modal != modalStateNotice {
		t.Fatal("opening the report's folder dismissed the notice")
	}
}

func TestOtherStartupNoticesDoNotOfferToOpenAFile(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.explainStateFile(loadReport{outcome: loadUnreadable, kept: filepath.Join(t.TempDir(), "state.json.corrupt")})
	canvas, _ := paintedTexts(state)
	if hasHit(canvas, hitStateNoticeReveal) {
		t.Fatalf("a notice about no file offers to open one: %+v", canvas.Hits())
	}
}
