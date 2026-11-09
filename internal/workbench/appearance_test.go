package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
)

// fakeSystem is a system look the test can switch, and the watch the workbench
// registers with it.
type fakeSystem struct {
	look     app.Appearance
	changed  func()
	stopped  bool
	watchers int
}

func (system *fakeSystem) options(options Options) Options {
	if options.StatePath == "" {
		options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	}
	options.SystemAppearance = func() app.Appearance { return system.look }
	options.WatchSystemAppearance = func(changed func()) func() {
		system.changed = changed
		system.watchers++
		return func() { system.stopped = true }
	}
	return options
}

// switchTo is the user changing the system's look: the workbench is told.
func (system *fakeSystem) switchTo(look app.Appearance) {
	system.look = look
	system.changed()
}

func TestFirstRunOpensDarkWhateverTheSystemShows(t *testing.T) {
	for _, look := range []app.Appearance{app.AppearanceLight, app.AppearanceDark, app.AppearanceUnknown} {
		system := &fakeSystem{look: look}
		state := newTestState(t, system.options(freshOptions(t)))
		if state.appearance != appearanceDark || state.appearanceMode != appearancePinnedDark {
			t.Errorf("system %v: look %v mode %v, want the dark default", look, state.appearance, state.appearanceMode)
		}
		if state.pal != appearanceDark.palette() {
			t.Errorf("system %v: the palette is not the dark one", look)
		}
	}
}

func TestASavedLookReadsBack(t *testing.T) {
	for _, test := range []struct {
		saved string
		want  appearanceMode
	}{
		{"", appearancePinnedDark},         // nothing saved: the default
		{"dark", appearancePinnedDark},     // chosen
		{"light", appearancePinnedLight},   // chosen
		{"system", appearanceFollowSystem}, // chosen, and written so it survives
		{"nonsense", appearancePinnedDark}, // an unknown word is not a choice
	} {
		if got := appearanceModeFromString(test.saved); got != test.want {
			t.Errorf("%q reads back as %v, want %v", test.saved, got, test.want)
		}
	}
}

func TestFollowingTheSystemIsWrittenSoItKeepsFollowing(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceLight}
	options := system.options(freshOptions(t))
	first := newTestState(t, options)
	first.Click(hitSettings)
	clickSettingsChoice(t, first, first.messages.SettingsAppearanceSystem)
	if first.appearance != appearanceLight {
		t.Fatalf("the chosen system is light but the look is %v", first.appearance)
	}
	first.save()
	if saved, _, _ := loadState(options.StatePath); saved.Theme != "system" {
		t.Fatalf("following the system was saved as %q", saved.Theme)
	}
	system.look = app.AppearanceDark
	second := newTestState(t, options)
	if second.appearance != appearanceDark || second.appearanceMode != appearanceFollowSystem {
		t.Fatalf("look = %v mode %v, want dark and still following after the system changed", second.appearance, second.appearanceMode)
	}
}

func TestTheWindowFollowsASwitchOfTheSystemWhileRunning(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceDark}
	options := system.options(freshOptions(t))
	repaints := 0
	options.Invalidate = func() { repaints++ }
	state := newTestState(t, options)
	if system.watchers != 1 {
		t.Fatalf("%d watchers registered, want one", system.watchers)
	}
	state.Click(hitSettings)
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceSystem)

	system.switchTo(app.AppearanceLight)
	if state.appearance != appearanceLight || state.Theme().Background != lightPalette.workspace || repaints != 1 {
		t.Fatalf("after the switch: look %v, background %v, %d repaints", state.appearance, state.Theme().Background, repaints)
	}
	system.switchTo(app.AppearanceLight) // told again, nothing changed
	if repaints != 1 {
		t.Fatalf("a call that changed nothing repainted (%d)", repaints)
	}
	system.switchTo(app.AppearanceDark)
	if state.appearance != appearanceDark || repaints != 2 {
		t.Fatalf("back to dark: look %v, %d repaints", state.appearance, repaints)
	}
}

func TestAPinnedLookIgnoresTheSystem(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceDark}
	options := system.options(freshOptions(t))
	state := newTestState(t, options)
	state.Click(hitSettings)
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceLight)
	if state.appearance != appearanceLight || state.appearanceMode != appearancePinnedLight {
		t.Fatalf("look %v mode %v", state.appearance, state.appearanceMode)
	}
	system.switchTo(app.AppearanceDark)
	system.switchTo(app.AppearanceLight)
	system.switchTo(app.AppearanceDark)
	if state.appearance != appearanceLight {
		t.Fatalf("the system's switches moved a pinned look to %v", state.appearance)
	}
	if saved, _, _ := loadState(options.StatePath); saved.Theme != "light" {
		t.Fatalf("a pinned look was saved as %q", saved.Theme)
	}
	again := newTestState(t, options)
	if again.appearance != appearanceLight || again.appearanceMode != appearancePinnedLight {
		t.Fatalf("after a restart: look %v mode %v", again.appearance, again.appearanceMode)
	}
}

func TestTheWindowBarButtonFlipsTheLookAndPinsIt(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceDark}
	state := newTestState(t, system.options(freshOptions(t)))
	state.Click(hitTheme)
	if state.appearance != appearanceLight || state.appearanceMode != appearancePinnedLight {
		t.Fatalf("after one press: look %v mode %v", state.appearance, state.appearanceMode)
	}
	state.Click(hitTheme)
	if state.appearance != appearanceDark || state.appearanceMode != appearancePinnedDark {
		t.Fatalf("after two presses: look %v mode %v", state.appearance, state.appearanceMode)
	}
	system.switchTo(app.AppearanceLight)
	if state.appearance != appearanceDark {
		t.Fatal("a look the user flipped to followed the system anyway")
	}
}

func TestChoosingSystemInSettingsFollowsItAtOnce(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceLight}
	state := newTestState(t, system.options(freshOptions(t)))
	state.Click(hitSettings)
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceDark)
	if state.appearance != appearanceDark {
		t.Fatal("setup: dark was not chosen")
	}
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceSystem)
	if state.appearanceMode != appearanceFollowSystem || state.appearance != appearanceLight {
		t.Fatalf("after choosing the system's: mode %v look %v, want following and light", state.appearanceMode, state.appearance)
	}
	system.switchTo(app.AppearanceDark)
	if state.appearance != appearanceDark {
		t.Fatal("the look no longer follows")
	}
	canvas, _ := paintedTexts(state)
	if _, ok := settingsChoiceIn(canvas, state.messages.SettingsAppearanceSystem); !ok {
		t.Fatal("the System choice is not painted")
	}
	if saved, _, _ := loadState(state.options.StatePath); saved.Theme != "system" {
		t.Fatalf("following the system was saved as %q", saved.Theme)
	}
}

func TestAnOlderStateFilesLookStaysChosen(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceLight}
	options := system.options(freshOptions(t))
	if err := saveState(options.StatePath, savedState{Theme: "dark"}); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if state.appearance != appearanceDark || state.appearanceMode != appearancePinnedDark {
		t.Fatalf("look %v mode %v, want the saved dark kept", state.appearance, state.appearanceMode)
	}
}

func TestClosingTheWorkbenchStopsListeningToTheSystem(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceDark}
	state := New(system.options(freshOptions(t)))
	state.Close()
	if !system.stopped {
		t.Fatal("the watcher is still registered after Close")
	}
	system.look = app.AppearanceLight
	system.changed() // a late call must not touch a closed state
	if state.appearance != appearanceDark {
		t.Fatal("a closed workbench still followed the system")
	}
}

func TestWhatTheSystemDrawsFollowsAPinnedLookAndIsGivenBackWhenFollowing(t *testing.T) {
	system := &fakeSystem{look: app.AppearanceDark}
	var asked []app.Appearance
	options := system.options(freshOptions(t))
	options.SetNativeAppearance = func(look app.Appearance) { asked = append(asked, look) }
	state := newTestState(t, options)
	if len(asked) == 0 || asked[len(asked)-1] != app.AppearanceDark {
		t.Fatalf("at start the dark default is pinned at the system too: %v", asked)
	}
	state.Click(hitSettings)
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceLight)
	if asked[len(asked)-1] != app.AppearanceLight {
		t.Fatalf("pinned light: %v", asked)
	}
	state.Click(hitTheme) // flips to dark and pins it
	if asked[len(asked)-1] != app.AppearanceDark {
		t.Fatalf("flipped to dark: %v", asked)
	}
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceSystem)
	if asked[len(asked)-1] != app.AppearanceUnknown {
		t.Fatalf("following the system again: %v", asked)
	}
	// A saved pin is applied at the next start.
	clickSettingsChoice(t, state, state.messages.SettingsAppearanceDark)
	asked = nil
	newTestState(t, options)
	if len(asked) == 0 || asked[0] != app.AppearanceDark {
		t.Fatalf("a saved dark look did not reach the system at start: %v", asked)
	}
}
