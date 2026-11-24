//go:build darwin || windows || linux

package workbench

import (
	"math"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func findTextBounds(texts []gfx.Text, value string) (gfx.Rect, gfx.Color, bool) {
	for _, item := range texts {
		if item.Value == value {
			return item.Bounds, item.Style.Color, true
		}
	}
	return gfx.Rect{}, gfx.Color{}, false
}

func rectMaxDiff(a, b gfx.Rect) float64 {
	return math.Max(
		math.Max(math.Abs(float64(a.X-b.X)), math.Abs(float64(a.Y-b.Y))),
		math.Max(math.Abs(float64(a.Width-b.Width)), math.Abs(float64(a.Height-b.Height))),
	)
}

// Regression for #1460: opening or closing a ui.Select inside the host dialog
// must animate only the dropdown popup, keeping surrounding form labels and
// footer controls stationary without fading or ghost replay.
func TestHostDialogSelectOpenDoesNotJitterPage(t *testing.T) {
	state, view := openIdleKeepAliveForm(t, protocolSSH)
	clickFormHit(t, view, hitDialogAdvanced)

	cases := []struct {
		name  string
		label string
	}{
		{name: "authentication", label: state.messages.FieldAuthentication},
		{name: "icon", label: state.messages.FieldIcon},
		{name: "keepalive", label: state.messages.Tools.IdleKeepAliveField},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			focusFormLabel(t, state, view, tc.label)
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}

			before := view.Snapshot()
			cancelBefore, cancelColorBefore, ok := findTextBounds(before.Texts, state.messages.Cancel)
			if !ok {
				t.Fatalf("cancel button text %q not found before opening %s", state.messages.Cancel, tc.name)
			}
			submitBefore, submitColorBefore, ok := findTextBounds(before.Texts, state.messages.AddHost)
			if !ok {
				t.Fatalf("submit button text %q not found before opening %s", state.messages.AddHost, tc.name)
			}
			groupBefore, groupColorBefore, hasGroup := findTextBounds(before.Texts, state.messages.FieldGroup)

			if err := view.ClickLabel(tc.label); err != nil {
				t.Fatalf("click %s: %v", tc.label, err)
			}
			if !view.Transitioning() {
				t.Fatalf("expected %s popup entrance transition to be active after opening", tc.name)
			}
			if err := view.Advance(16 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			opening := view.Snapshot()
			cancelOpening, cancelColorOpening, ok := findTextBounds(opening.Texts, state.messages.Cancel)
			if !ok {
				t.Fatalf("cancel button text %q disappeared while opening %s", state.messages.Cancel, tc.name)
			}
			if diff := rectMaxDiff(cancelBefore, cancelOpening); diff > 0.01 || math.Abs(float64(cancelColorBefore.Alpha-cancelColorOpening.Alpha)) > 0.01 {
				t.Errorf("opening %s jittered Cancel button: before=%+v (alpha=%.2f), opening=%+v (alpha=%.2f), maxDiff=%.2fpx",
					tc.name, cancelBefore, cancelColorBefore.Alpha, cancelOpening, cancelColorOpening.Alpha, diff)
			}
			submitOpening, submitColorOpening, ok := findTextBounds(opening.Texts, state.messages.AddHost)
			if !ok {
				t.Fatalf("submit button text %q disappeared while opening %s", state.messages.AddHost, tc.name)
			}
			if diff := rectMaxDiff(submitBefore, submitOpening); diff > 0.01 || math.Abs(float64(submitColorBefore.Alpha-submitColorOpening.Alpha)) > 0.01 {
				t.Errorf("opening %s jittered Submit button: before=%+v (alpha=%.2f), opening=%+v (alpha=%.2f), maxDiff=%.2fpx",
					tc.name, submitBefore, submitColorBefore.Alpha, submitOpening, submitColorOpening.Alpha, diff)
			}
			if hasGroup {
				if groupOpening, groupColorOpening, ok := findTextBounds(opening.Texts, state.messages.FieldGroup); ok {
					if diff := rectMaxDiff(groupBefore, groupOpening); diff > 0.01 || math.Abs(float64(groupColorBefore.Alpha-groupColorOpening.Alpha)) > 0.01 {
						t.Errorf("opening %s jittered FieldGroup label: before=%+v (alpha=%.2f), opening=%+v (alpha=%.2f), maxDiff=%.2fpx",
							tc.name, groupBefore, groupColorBefore.Alpha, groupOpening, groupColorOpening.Alpha, diff)
					}
				}
			}

			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if err := view.Keys("Escape"); err != nil {
				t.Fatal(err)
			}
			if err := view.Advance(16 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			closing := view.Snapshot()
			cancelCount := 0
			for _, item := range closing.Texts {
				if item.Value == state.messages.Cancel {
					cancelCount++
				}
			}
			if cancelCount != 1 {
				t.Errorf("closing %s painted %d copies of Cancel button text, want 1", tc.name, cancelCount)
			}
		})
	}
}

// Regression for #1460 inside ui.Drawer: opening a ui.Select in the settings
// drawer must not jitter or fade subsequent drawer controls.
func TestSettingsDrawerSelectOpenDoesNotJitterPage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.settingsCategory = settingsConnection

	view, err := headless.Open(app.Config{
		Width: 1100, Height: 1400, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnResize: state.Resize,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	before := view.Snapshot()
	syncBefore, syncColorBefore, ok := findTextBounds(before.Texts, state.messages.SSHSync.Group)
	if !ok {
		t.Fatalf("settings heading %q not found before opening select", state.messages.SSHSync.Group)
	}

	if err := view.ClickLabel(state.messages.SettingsSSHBrowser); err != nil {
		t.Fatalf("click SettingsSSHBrowser select: %v", err)
	}
	if err := view.Advance(16 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	opening := view.Snapshot()
	syncOpening, syncColorOpening, ok := findTextBounds(opening.Texts, state.messages.SSHSync.Group)
	if !ok {
		t.Fatalf("settings heading %q disappeared while opening select", state.messages.SSHSync.Group)
	}
	if diff := rectMaxDiff(syncBefore, syncOpening); diff > 0.01 || math.Abs(float64(syncColorBefore.Alpha-syncColorOpening.Alpha)) > 0.01 {
		t.Errorf("opening settings select jittered drawer text: before=%+v (alpha=%.2f), opening=%+v (alpha=%.2f), maxDiff=%.2fpx",
			syncBefore, syncColorBefore.Alpha, syncOpening, syncColorOpening.Alpha, diff)
	}
}
