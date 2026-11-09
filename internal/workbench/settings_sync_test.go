package workbench

import (
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/agent"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
)

// windowsOnOneStore makes two windows of one process over one store, the way the
// application does: both read and write the same configuration file, so a save
// in one is what the other has to pick up.
type windowsOnOneStore struct {
	first, second *State
	firstDialer   *world
	secondDialer  *world
	firstWakes    atomic.Int32
	secondWakes   atomic.Int32
}

func newWindowsOnOneStore(t *testing.T) *windowsOnOneStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := saveState(path, savedState{Hosts: sampleHosts}); err != nil {
		t.Fatal(err)
	}
	group := NewGroup()
	pair := &windowsOnOneStore{}
	makeOne := func(id int, wakes *atomic.Int32) (*State, *world) {
		dialer, options := newWorld(t)
		options.StatePath = path
		options.KnownHostsPath = filepath.Join(dir, "known_hosts")
		options.Group = group
		// Two windows of one process, as the application makes them: each keeps
		// its own tabs under its own number.
		options.WindowID = id
		options.Invalidate = func() { wakes.Add(1) }
		return newTestState(t, options), dialer
	}
	pair.first, pair.firstDialer = makeOne(1, &pair.firstWakes)
	pair.second, pair.secondDialer = makeOne(2, &pair.secondWakes)
	return pair
}

// A setting changed in one window is what the other windows show as well, both a
// setting every computer shares and one of this device's own.
func TestASettingChangedInOneWindowReachesTheOthers(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	if pair.second.notifyOff {
		t.Fatal("the second window does not start with notifications on")
	}
	if !pair.first.setNotifications(false) {
		t.Fatal("turning notifications off changed nothing")
	}
	pair.first.save()
	if !pair.second.notifyOff {
		t.Fatal("the second window still shows notifications on")
	}

	if !pair.first.chooseAppearanceMode(appearancePinnedLight) {
		t.Fatal("choosing the light look changed nothing")
	}
	pair.first.save()
	if pair.second.appearanceMode != appearancePinnedLight {
		t.Fatalf("the second window is still in look %v", pair.second.appearanceMode)
	}
}

// Reading the change back takes the settings and the shared records, not what
// the window has open of its own.
func TestAWindowKeepsItsOwnTabsWhenAnotherWindowChangesSettings(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	liveTab(t, pair.second, pair.secondDialer, 0)
	opened := tabOrder(pair.second)
	if len(opened) == 0 {
		t.Fatal("the second window has no tab open")
	}

	if !pair.first.setNotifications(false) {
		t.Fatal("turning notifications off changed nothing")
	}
	pair.first.save()

	if !pair.second.notifyOff {
		t.Fatal("the second window did not read the setting back")
	}
	if got := tabOrder(pair.second); !slices.Equal(got, opened) {
		t.Fatalf("the second window's tabs are %v, were %v", got, opened)
	}
	if len(pair.second.sessions) == 0 {
		t.Fatal("the second window lost its session")
	}
}

// Opening a tab is this window's own work: the save that records it has nothing
// in it for the other windows to read. What reading the store back leaves behind
// is a replaced base, so holding the same one is how this window says it did not.
func TestATabOpenedInOneWindowLeavesTheOthersAlone(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	pair.first.save()
	base := pair.second.configBase
	before := pair.first.snapshot()

	liveTab(t, pair.first, pair.firstDialer, 0)
	pair.first.save()

	if changes := changedSavedFields(before, pair.first.snapshot()); len(changes) == 0 {
		t.Fatal("opening a tab left nothing to save")
	}
	if pair.second.configBase != base {
		t.Fatal("opening a tab made the other window read the configuration back")
	}
}

func assistantWindowsOnOneStore(t *testing.T) ([3]*State, *AIHub) {
	t.Helper()
	_, options := newWorld(t)
	options.Group = NewGroup()
	options.AIStorePath = filepath.Join(filepath.Dir(options.StatePath), "ai.db")
	hub := NewAIHub(options.AIStorePath)
	t.Cleanup(hub.Close)
	options.AISettingsChanged = func() {
		if err := hub.Reconfigure(); err != nil {
			t.Fatal(err)
		}
	}
	var windows [3]*State
	for index := range windows {
		options.WindowID = index + 1
		state := newTestState(t, options)
		hub.Add(state, nil)
		state.Click(hitSettings)
		state.selectSettingsCategory(settingsAssistant)
		state.Wake()
		windows[index] = state
	}
	return windows, hub
}

func TestAssistantSettingsChangedInOneWindowReachOtherWindows(t *testing.T) {
	windows, hub := assistantWindowsOnOneStore(t)
	for _, change := range []struct {
		window int
		hit    gfx.ID
	}{
		{0, hitAISetSteps24}, {0, hitAISetContext0}, {1, hitAISetRiskHigh},
		{2, hitAISetRedact}, {0, hitAISetHistory}, {1, hitAISetSteps6},
		{1, hitAISetRedact}, {2, hitAISetRiskLow},
	} {
		origin := windows[change.window]
		before := origin.ai.settings
		origin.Click(change.hit)
		want := origin.ai.settings
		if want == before {
			t.Fatalf("setting %v did not change", change.hit)
		}
		for index, state := range windows {
			state.Wake()
			if got := state.ai.settings; got != want {
				t.Errorf("setting %v: window %d has %+v, want %+v from window %d", change.hit, index+1, got, want, change.window+1)
			}
		}
	}
	if hub.Address() != "" {
		t.Fatal("changing assistant behavior enabled the HTTP service")
	}
}

func TestAssistantDefaultModelChangesReachOtherWindows(t *testing.T) {
	windows, _ := assistantWindowsOnOneStore(t)
	db := windows[0].ai.store
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "Local test", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels("p", []store.Model{{Name: "first", Enabled: true}, {Name: "second", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	windows[0].aiWrote()
	for index, want := range []string{"first", "second"} {
		origin := windows[index]
		origin.ai.cfg.selected = "p"
		origin.Click(hitAISetModelUse.At(index))
		for number, state := range windows {
			state.Wake()
			if provider, model, err := state.aiModel(); err != nil || provider.ID != "p" || model != want {
				t.Fatalf("window %d selected model = %s/%s, %v; want p/%s", number+1, provider.ID, model, err, want)
			}
		}
	}
}

func TestAssistantSettingsRefreshKeepsAnActiveRequestAndDrafts(t *testing.T) {
	windows, _ := assistantWindowsOnOneStore(t)
	a, b := windows[0], windows[1]
	db := a.ai.store
	if err := db.SaveProvider(store.Provider{ID: "p", Name: "Local test", Kind: store.KindOpenAI, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDefaultModel("p", "first"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("ai.mode", "ask"); err != nil {
		t.Fatal(err)
	}
	a.aiWrote()
	b.Wake()
	started, finish := make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(finish) }) })
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		close(started)
		<-finish
		return textTurn("request finished")
	}}
	b.options.AIProvider = func(store.Provider) llm.Provider { return model }
	ask(b, "keep working", agent.ModeAgent)
	pumpUntil(t, b, "the model request", func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	})
	run := b.ai.run
	scope := risk.High
	b.ai.autoUpTo = &scope
	b.ai.input.Insert("next question")
	b.Click(hitAISetProviderAdd)
	b.ai.cfg.name.Insert("unsaved provider")
	a.Click(hitAISetSteps24)
	b.Wake()
	if b.ai.settings.maxSteps != 24 {
		t.Fatal("the window running a request did not receive the setting")
	}
	if b.ai.run != run || b.ai.mode != agent.ModeAgent || b.ai.autoUpTo != &scope {
		t.Fatal("the settings refresh replaced the active request or its conversation policy")
	}
	if b.ai.input.String() != "next question" || b.ai.cfg.dialog != aiDialogProvider || b.ai.cfg.name.String() != "unsaved provider" {
		t.Fatal("the settings refresh discarded an unsent question or provider draft")
	}
	release.Do(func() { close(finish) })
	pumpUntil(t, b, "the request to finish", func() bool { return b.ai.run == nil })
	if len(b.ai.items) == 0 || b.ai.items[len(b.ai.items)-1].text != "request finished" {
		t.Fatal("the active request did not deliver its answer after the refresh")
	}
}

func TestAssistantSettingsPublicationLeavesUnusedStoresClosed(t *testing.T) {
	windows, hub := assistantWindowsOnOneStore(t)
	options := windows[0].options
	options.WindowID = 4
	dormant := newTestState(t, options)
	hub.Add(dormant, nil)
	windows[0].Click(hitAISetSteps24)
	dormant.Wake()
	if dormant.ai.store != nil || dormant.ai.storeTried {
		t.Fatal("publishing settings opened an unused assistant database")
	}
	dormant.Click(hitSettings)
	if dormant.ai.settings.maxSteps != 24 {
		t.Fatal("opening the settings page did not read the latest choice")
	}
}

func TestAssistantExportChoicesReachOtherWindows(t *testing.T) {
	windows, hub := assistantWindowsOnOneStore(t)
	windows[0].Click(aiMCPClientID(aiMCPCodex))
	windows[1].Click(aiAgentClientID(aiMCPCursor))
	for index, state := range windows {
		if got := state.ai.cfg.mcpClient; got != aiMCPCodex {
			t.Fatalf("window %d HTTP export client = %v, want Codex", index+1, got)
		}
		if got := state.ai.cfg.agentClient; got != aiMCPCursor {
			t.Fatalf("window %d stdio export client = %v, want Cursor", index+1, got)
		}
	}
	if hub.Address() != "" {
		t.Fatal("changing an export choice enabled the HTTP service")
	}
}
