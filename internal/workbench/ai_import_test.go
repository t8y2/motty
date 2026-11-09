package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/ai/store"
	"motty/internal/aiproviderimport"
)

func importCandidate(name, kind, base, key string, models ...string) aiproviderimport.Candidate {
	return aiproviderimport.Candidate{
		Source: aiproviderimport.SourceCCSwitch, Name: name, Kind: kind, BaseURL: base, Key: key, Models: models,
	}
}

func importSource(tool aiproviderimport.Tool, path string, candidates ...aiproviderimport.Candidate) aiproviderimport.SourceInfo {
	return aiproviderimport.SourceInfo{Tool: tool, Path: path, Found: true, Candidates: candidates}
}

func importOpen(t *testing.T, world *settingsWorld) {
	t.Helper()
	world.state.Click(hitAISetProviderImport)
	if world.state.ai.cfg.dialog != aiDialogImport || world.state.ai.cfg.importStep != aiImportSources {
		t.Fatalf("import dialog = %d step = %d", world.state.ai.cfg.dialog, world.state.ai.cfg.importStep)
	}
}

func importPreview(t *testing.T, world *settingsWorld, index int) {
	t.Helper()
	world.state.Click(hitAISetImportSource.At(index))
	if world.state.ai.cfg.importStep != aiImportPreview {
		t.Fatalf("step = %d after choosing source %d", world.state.ai.cfg.importStep, index)
	}
}

func importApply(t *testing.T, world *settingsWorld) {
	t.Helper()
	world.state.Click(hitAISetImportApply)
	if world.state.ai.cfg.importStep != aiImportDone {
		t.Fatalf("step = %d after applying the import (err %q)", world.state.ai.cfg.importStep, world.state.ai.cfg.importErr)
	}
}

func TestAISettingsImportButtonReadsSourcesOnlyWhenPressed(t *testing.T) {
	scans := 0
	candidates := []aiproviderimport.Candidate{
		importCandidate("Relay", store.KindAnthropic, "https://relay.example.com", "fake-key", "m1"),
		importCandidate("Chat", store.KindOpenAI, "https://chat.example.com/v1", "fake-key", "g1"),
		{Source: aiproviderimport.SourceCCSwitch, Name: "Gemini", SkipReason: aiproviderimport.ReasonGemini},
	}
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.AIProviderScan = func() []aiproviderimport.SourceInfo {
			scans++
			return []aiproviderimport.SourceInfo{
				{Tool: aiproviderimport.ToolMagpie, Path: "/tmp/magpie/providers.json"},
				importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch/cc-switch.db", candidates...),
			}
		}
	})
	state := world.state

	canvas, _ := world.paint()
	if scans != 0 {
		t.Fatalf("opening Settings read the sources %d times", scans)
	}
	if hit, ok := hitByID(canvas, hitAISetProviderImport); !ok || hit.Disabled {
		t.Fatalf("import button = %+v, ok=%v", hit, ok)
	}
	if _, ok := hitByID(canvas, hitAISetProviderAdd); !ok {
		t.Fatal("the add-provider button disappeared")
	}

	world.paint()
	if scans != 0 {
		t.Fatalf("painting the page read the sources %d times", scans)
	}

	world.state.Click(hitAISetProviderImport)
	if scans != 1 {
		t.Fatalf("import read the sources %d times, want 1", scans)
	}
	_, texts := world.paint()
	m := state.messages.AISettings
	for _, want := range []string{
		m.ImportSourcesTitle, "magpie", "cc-switch", m.ImportSourceMissing,
		fmt.Sprintf(m.ImportSourceCountFmt, 2, 3),
	} {
		if !contains(texts, want) {
			t.Fatalf("%q is not painted: %v", want, texts)
		}
	}
	world.paint()
	if scans != 1 {
		t.Fatalf("painting the import dialog read the sources again: %d", scans)
	}

	world.state.Click(hitAISetImportClose)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatal("the import dialog did not close")
	}
	world.state.Click(hitAISetProviderAdd)
	if state.ai.cfg.dialog != aiDialogProvider {
		t.Fatal("the add-provider button no longer opens the form")
	}
}

func TestAISettingsImportChoosesAFileAndIdentifiesItsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.json")
	document := `{"providers":[{"id":"relay","name":"Relay","key":"fake-key","anthropic":"https://relay.example.com","models":["m1"]}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	pick, picks, pickedTitle := path, 0, ""
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.AIProviderScan = func() []aiproviderimport.SourceInfo {
			return []aiproviderimport.SourceInfo{
				{Tool: aiproviderimport.ToolMagpie, Path: "/missing/providers.json"},
				{Tool: aiproviderimport.ToolCCSwitch, Path: "/missing/cc-switch.db"},
			}
		}
		options.OpenFiles = func(request app.OpenFileOptions) ([]string, error) {
			picks++
			pickedTitle = request.Title
			if pick == "" {
				return nil, app.ErrCanceled
			}
			return []string{pick}, nil
		}
	})
	state := world.state
	importOpen(t, world)
	_, texts := world.paint()
	if !contains(texts, state.messages.AISettings.ImportSourceMissing) {
		t.Fatalf("missing sources are not shown: %v", texts)
	}

	world.state.Click(hitAISetImportChoose)
	if picks != 1 || state.ai.cfg.importStep != aiImportPreview {
		t.Fatalf("picks=%d step=%d", picks, state.ai.cfg.importStep)
	}
	if pickedTitle != state.messages.AISettings.ImportChooseTitle {
		t.Fatalf("picker title = %q", pickedTitle)
	}
	// The gateway and the upstream endpoint are both offered.
	if len(state.ai.cfg.importCandidates) != 2 {
		t.Fatalf("candidates = %#v", state.ai.cfg.importCandidates)
	}
	world.state.Click(hitAISetImportBack)
	if state.ai.cfg.importStep != aiImportSources {
		t.Fatal("Back did not return to the source step")
	}

	pick = ""
	world.state.Click(hitAISetImportChoose)
	if state.ai.cfg.importStep != aiImportSources || state.ai.cfg.importErr != "" {
		t.Fatalf("a canceled picker changed state: step=%d err=%q", state.ai.cfg.importStep, state.ai.cfg.importErr)
	}
	if pick != "" {
		t.Fatal("the canceled path was used")
	}
}

func TestAISettingsImportPreviewListsEveryCandidateAndWritesNothingWhenEmpty(t *testing.T) {
	var importable []aiproviderimport.Candidate
	for index := 0; index < 9; index++ {
		importable = append(importable, importCandidate(
			fmt.Sprintf("Relay %d", index), store.KindOpenAI,
			fmt.Sprintf("https://relay%d.example.com/v1", index), "", "m1",
		))
	}
	// The unavailable entry starts in the middle of the source order to prove
	// that the preview moves it to the end and never paginates.
	disabled := aiproviderimport.Candidate{
		Source: aiproviderimport.SourceCCSwitch, Name: "Disabled", SkipReason: aiproviderimport.ReasonDisabled,
	}
	candidates := append([]aiproviderimport.Candidate{}, importable[:4]...)
	candidates = append(candidates, disabled)
	candidates = append(candidates, importable[4:]...)
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.AIProviderScan = func() []aiproviderimport.SourceInfo {
			return []aiproviderimport.SourceInfo{
				importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db", candidates...),
				{Tool: aiproviderimport.ToolMagpie, Path: "/missing"},
			}
		}
	})
	state := world.state
	importOpen(t, world)
	importPreview(t, world, 0)
	if got := state.aiImportSelectedCount(); got != 9 {
		t.Fatalf("selected = %d, want 9", got)
	}
	if got := aiImportImportableCount(state.ai.cfg.importCandidates); got != 9 {
		t.Fatalf("importable = %d", got)
	}
	if got := state.ai.cfg.importCandidates[9]; got.Name != "Disabled" || got.Importable() {
		t.Fatalf("the unavailable entry is not last: %#v", got)
	}
	for index := 0; index < 9; index++ {
		if !state.ai.cfg.importCandidates[index].Importable() {
			t.Fatalf("candidate %d is not importable: %#v", index, state.ai.cfg.importCandidates[index])
		}
	}

	canvas, texts := world.paint()
	if !contains(texts, state.messages.AISettings.ImportReasonDisabled) {
		t.Fatalf("the disabled reason is not painted: %v", texts)
	}
	if _, ok := hitByID(canvas, hitAISetImportRow.At(9)); ok {
		t.Fatal("an unavailable row still paints a checkbox")
	}
	if _, ok := hitByID(canvas, hitAISetImportRow.At(0)); !ok {
		t.Fatal("the first importable row has no checkbox")
	}
	world.state.Click(hitAISetImportRow.At(9))
	if got := state.aiImportSelectedCount(); got != 9 {
		t.Fatalf("clicking an unavailable row selected it: %d", got)
	}

	// The whole list is present; the wheel moves it instead of the page.
	if hit, ok := hitByID(canvas, hitAISetImportList); !ok {
		t.Fatal("the preview list has no scroll group")
	} else if scroll := state.settingsRow.ScrollState(hitAISetImportList); scroll == nil || scroll.Max() <= 0 {
		t.Fatal("the preview list does not scroll when it is taller than its viewport")
	} else {
		state.Scroll(gfx.ScrollEvent{X: hit.Bounds.X + 20, Y: hit.Bounds.Y + 20, DeltaY: 120, Precise: true})
		if scroll.Offset == 0 {
			t.Fatal("the wheel over the preview list did not scroll it")
		}
		canvas, _ = world.paint()
		if _, ok := hitByID(canvas, hitAISetImportRow.At(8)); !ok {
			t.Fatal("scrolling did not reveal the last importable row")
		}
	}

	world.state.Click(hitAISetImportRow.At(0))
	if got := state.aiImportSelectedCount(); got != 8 {
		t.Fatalf("deselecting left %d", got)
	}
	world.state.Click(hitAISetImportRow.At(0))
	if got := state.aiImportSelectedCount(); got != 9 {
		t.Fatalf("reselecting left %d", got)
	}

	for index := 0; index < 9; index++ {
		world.state.Click(hitAISetImportRow.At(index))
	}
	if got := state.aiImportSelectedCount(); got != 0 {
		t.Fatalf("selection after clearing = %d", got)
	}
	canvas, _ = world.paint()
	if hit, ok := hitByID(canvas, hitAISetImportApply); !ok || !hit.Disabled {
		t.Fatalf("Import with an empty selection = %+v, ok=%v", hit, ok)
	}
	world.state.Click(hitAISetImportApply)
	if state.ai.cfg.importStep != aiImportPreview {
		t.Fatal("an empty selection left the preview")
	}
	if len(state.ai.providers) != 0 {
		t.Fatalf("an empty selection wrote providers: %v", state.ai.providers)
	}
}

func TestAISettingsImportCreatesProvidersAndSelectsTheFirstModel(t *testing.T) {
	anthropic := importCandidate("Relay", store.KindAnthropic, "https://relay.example.com", "secret-anthropic", "m2", "m1")
	anthropic.AuthStyle = aiproviderimport.AuthStyleBearer
	openAI := importCandidate("Chat", store.KindOpenAI, "https://chat.example.com/v1", "", "g1")
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.AIProviderScan = func() []aiproviderimport.SourceInfo {
			return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db", anthropic, openAI)}
		}
	})
	state := world.state
	importOpen(t, world)
	importPreview(t, world, 0)
	_, previewTexts := world.paint()
	for _, want := range []string{"relay.example.com", "chat.example.com", state.messages.AISettings.ProviderKeySaved, state.messages.AISettings.ProviderNoKey} {
		if !contains(previewTexts, want) {
			t.Fatalf("%q is not in the preview: %v", want, previewTexts)
		}
	}
	for _, text := range previewTexts {
		if strings.Contains(text, "secret-anthropic") {
			t.Fatalf("the preview shows a credential: %q", text)
		}
	}
	importApply(t, world)

	if len(state.ai.providers) != 2 {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	var relay, chat store.Provider
	for _, provider := range state.ai.providers {
		switch provider.Name {
		case "Relay":
			relay = provider
		case "Chat":
			chat = provider
		}
	}
	if relay.ID == "" || relay.Kind != store.KindAnthropic || relay.BaseURL != "https://relay.example.com" ||
		relay.AuthStyle != store.AuthStyleBearer || !relay.HasKey || !relay.Enabled {
		t.Fatalf("Relay = %#v", relay)
	}
	if chat.ID == "" || chat.Kind != store.KindOpenAI || chat.HasKey || chat.BaseURL != "https://chat.example.com/v1" {
		t.Fatalf("Chat = %#v", chat)
	}
	if key, found, err := state.secrets.vault.Get(aiKeySecretName(relay.ID)); err != nil || !found || key != "secret-anthropic" {
		t.Fatalf("vault holds %q found=%v err=%v", key, found, err)
	}
	models := state.aiModelList(relay.ID)
	if len(models) != 2 || models[0].Name != "m1" || !models[0].Enabled || models[1].Name != "m2" || !models[1].Enabled {
		t.Fatalf("Relay models = %#v", models)
	}
	if provider, name, ok, err := world.db(t).DefaultModel(); err != nil || !ok || provider != relay.ID || name != "m2" {
		t.Fatalf("default = %q/%q ok=%v err=%v, want %q/m2", provider, name, ok, err, relay.ID)
	}
	result := state.ai.cfg.importResult
	if result.Created != 2 || result.Updated != 0 || result.Skipped != 0 || result.KeyFailed != 0 {
		t.Fatalf("result = %#v", result)
	}
	if world.changed != 1 {
		t.Fatalf("the application was told %d times", world.changed)
	}
	world.lister.mu.Lock()
	networkCalls := world.lister.calls
	world.lister.mu.Unlock()
	if networkCalls != 0 {
		t.Fatalf("the import called a provider %d times", networkCalls)
	}

	_, texts := world.paint()
	if !contains(texts, fmt.Sprintf(state.messages.AISettings.ImportSummaryFmt, 2, 0, 0)) {
		t.Fatalf("the summary is missing: %v", texts)
	}
	for _, text := range texts {
		if strings.Contains(text, "secret-anthropic") {
			t.Fatalf("a credential is painted: %q", text)
		}
	}
}

func TestAISettingsImportKeepsAnExistingDefaultModel(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), nil)
	state := world.state
	existing := store.Provider{ID: "pold", Name: "Old", Kind: store.KindOpenAI, BaseURL: "https://old.example.com/v1", Enabled: true}
	if err := world.db(t).SaveProvider(existing); err != nil {
		t.Fatal(err)
	}
	if err := world.db(t).SaveModels(existing.ID, []store.Model{{ProviderID: existing.ID, Name: "old", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := world.db(t).SetDefaultModel(existing.ID, "old"); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	world.options.AIProviderScan = func() []aiproviderimport.SourceInfo {
		return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db",
			importCandidate("New", store.KindOpenAI, "https://new.example.com/v1", "", "new-model"))}
	}
	// The seam is read from state.options, which was copied at New; install it
	// directly so this test does not need a second window.
	state.options.AIProviderScan = world.options.AIProviderScan
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)
	if provider, name, ok, err := world.db(t).DefaultModel(); err != nil || !ok || provider != existing.ID || name != "old" {
		t.Fatalf("default = %q/%q ok=%v err=%v", provider, name, ok, err)
	}
}

func TestAISettingsImportMatchesProvidersAndAdoptsTheKeyStyle(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), nil)
	state := world.state
	seed := store.Provider{ID: "p1", Name: "Relay", Kind: store.KindAnthropic, BaseURL: "https://relay.example.com", Enabled: true}
	if err := world.db(t).SaveProvider(seed); err != nil {
		t.Fatal(err)
	}
	if err := world.db(t).SaveModels(seed.ID, []store.Model{{ProviderID: seed.ID, Name: "m0", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	candidate := importCandidate("Relay", store.KindAnthropic, "https://relay.example.com/", "secret-new", "m0", "m1")
	candidate.AuthStyle = aiproviderimport.AuthStyleBearer
	state.options.AIProviderScan = func() []aiproviderimport.SourceInfo {
		return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db", candidate)}
	}
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)

	if len(state.ai.providers) != 1 {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	got := state.ai.providers[0]
	if !got.HasKey || got.AuthStyle != aiproviderimport.AuthStyleBearer {
		t.Fatalf("provider = %#v: the key must keep the source's bearer header", got)
	}
	if key, found, err := state.secrets.vault.Get(aiKeySecretName(got.ID)); err != nil || !found || key != "secret-new" {
		t.Fatalf("vault holds %q found=%v err=%v", key, found, err)
	}
	models := state.aiModelList(got.ID)
	if len(models) != 2 || models[0].Name != "m0" || !models[0].Enabled || models[1].Name != "m1" || models[1].Enabled {
		t.Fatalf("models = %#v, want m0 enabled and m1 added disabled", models)
	}
	if state.ai.cfg.importResult.Updated != 1 || state.ai.cfg.importResult.Created != 0 {
		t.Fatalf("result = %#v", state.ai.cfg.importResult)
	}

	// The same data again changes nothing and creates nothing.
	state.Click(hitAISetImportClose)
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)
	if len(state.ai.providers) != 1 || len(state.aiModelList(got.ID)) != 2 {
		t.Fatalf("re-import duplicated data: %#v %#v", state.ai.providers, state.aiModelList(got.ID))
	}
	if result := state.ai.cfg.importResult; result.Created != 0 || result.Updated != 0 || result.Skipped != 1 {
		t.Fatalf("re-import result = %#v", result)
	}
}

func TestAISettingsImportMatchesATruncatedNumberedName(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), nil)
	state := world.state
	longName := strings.Repeat("Relay name ", 6) // 66 runes, trimmed by the 64-rune limit
	seed := store.Provider{ID: "p1", Name: truncateRunes(longName, aiproviderimport.MaxNameRunes), Kind: store.KindAnthropic, BaseURL: "https://one.example.com", Enabled: true}
	if err := world.db(t).SaveProvider(seed); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	candidate := importCandidate(seed.Name, store.KindAnthropic, "https://two.example.com", "", "m1")
	state.options.AIProviderScan = func() []aiproviderimport.SourceInfo {
		return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db", candidate)}
	}
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)

	if len(state.ai.providers) != 2 {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	want := truncateRunes(seed.Name, aiproviderimport.MaxNameRunes-len([]rune(" (2)"))) + " (2)"
	var numbered bool
	for _, provider := range state.ai.providers {
		if provider.BaseURL == "https://two.example.com" && provider.Name == want {
			numbered = true
		}
	}
	if !numbered {
		t.Fatalf("no truncated numbered provider %q: %#v", want, state.ai.providers)
	}

	// The numbered name is the same provider on the next import, even though
	// the source name is longer than the stored one.
	state.Click(hitAISetImportClose)
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)
	if len(state.ai.providers) != 2 {
		t.Fatalf("re-import duplicated the long numbered provider: %#v", state.ai.providers)
	}
	if result := state.ai.cfg.importResult; result.Created != 0 || result.Skipped != 1 {
		t.Fatalf("re-import result = %#v", result)
	}
}

func TestAISettingsImportKeepsALongEndpointIntactInTheEditor(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), nil)
	state := world.state
	base := "https://" + strings.Repeat("a", 350) + ".example.com/v1"
	candidate := importCandidate("Long Relay", store.KindOpenAI, base, "", "m1")
	state.options.AIProviderScan = func() []aiproviderimport.SourceInfo {
		return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db", candidate)}
	}
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)
	state.Click(hitAISetImportClose)
	if len(state.ai.providers) != 1 {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	state.Click(hitAISetProviderEdit.At(0))
	if got := state.ai.cfg.baseURL.String(); got != base {
		t.Fatalf("edit field = %q (%d runes), want the imported endpoint", got, len([]rune(got)))
	}
}

func TestAISettingsImportKeepsAnExistingCredentialAndDisambiguatesNames(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), nil)
	state := world.state
	seed := store.Provider{ID: "p1", Name: "Relay", Kind: store.KindAnthropic, BaseURL: "https://relay.example.com", HasKey: true, Enabled: true}
	if err := world.db(t).SaveProvider(seed); err != nil {
		t.Fatal(err)
	}
	if err := state.aiSaveKey(seed.ID, "secret-old"); err != nil {
		t.Fatal(err)
	}
	if err := world.db(t).SaveModels(seed.ID, []store.Model{{ProviderID: seed.ID, Name: "m0", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()

	// Same endpoint and name: the saved key is kept, the new model appended.
	same := importCandidate("Relay", store.KindAnthropic, "https://relay.example.com", "secret-new", "m0", "m1")
	// Same name at another endpoint: a new provider gets a numbered name.
	other := importCandidate("Relay", store.KindAnthropic, "https://other.example.com", "", "x")
	state.options.AIProviderScan = func() []aiproviderimport.SourceInfo {
		return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db", same, other)}
	}
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)

	if len(state.ai.providers) != 2 {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	if key, found, err := state.secrets.vault.Get(aiKeySecretName(seed.ID)); err != nil || !found || key != "secret-old" {
		t.Fatalf("the existing key was replaced: %q found=%v err=%v", key, found, err)
	}
	var numbered bool
	for _, provider := range state.ai.providers {
		if provider.Name == "Relay (2)" && provider.BaseURL == "https://other.example.com" {
			numbered = true
		}
	}
	if !numbered {
		t.Fatalf("no disambiguated provider: %#v", state.ai.providers)
	}
	if result := state.ai.cfg.importResult; result.Created != 1 || result.Updated != 1 || result.Skipped != 0 {
		t.Fatalf("result = %#v", result)
	}

	// Re-importing matches the numbered provider instead of adding a third.
	state.Click(hitAISetImportClose)
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)
	if len(state.ai.providers) != 2 {
		t.Fatalf("re-import duplicated the numbered provider: %#v", state.ai.providers)
	}
	if result := state.ai.cfg.importResult; result.Created != 0 || result.Skipped != 2 {
		t.Fatalf("re-import result = %#v", result)
	}
}

func TestAISettingsImportWaitsForALockedVault(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.AIProviderScan = func() []aiproviderimport.SourceInfo {
			return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db",
				importCandidate("Locked", store.KindOpenAI, "https://locked.example.com/v1", "secret-locked", "m1"))}
		}
	})
	state := world.state
	if err := state.secrets.vault.LegacyForTests("master password"); err != nil {
		t.Fatal(err)
	}
	state.secrets.declined = true
	state.secrets.vault.DropKeyForTests()
	importOpen(t, world)
	importPreview(t, world, 0)
	state.Click(hitAISetImportApply)

	if state.modal != modalVaultUnlock {
		t.Fatalf("modal = %d, want the unlock dialog", state.modal)
	}
	if len(state.ai.providers) != 0 || state.ai.cfg.importStep != aiImportPreview {
		t.Fatal("the import wrote before the vault was unlocked")
	}
	typeInto(state, hitVaultUnlockField, "master password")
	state.Click(hitVaultUnlockSubmit)
	if state.modal != modalNone || state.ai.cfg.importStep != aiImportDone {
		t.Fatalf("modal=%d step=%d after unlocking", state.modal, state.ai.cfg.importStep)
	}
	if len(state.ai.providers) != 1 || !state.ai.providers[0].HasKey {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	if key, found, err := state.secrets.vault.Get(aiKeySecretName(state.ai.providers[0].ID)); err != nil || !found || key != "secret-locked" {
		t.Fatalf("vault holds %q found=%v err=%v", key, found, err)
	}
}

func TestAISettingsImportReportsAProviderSavedWithoutItsKey(t *testing.T) {
	world := newSettingsWorldWith(t, newLister(), func(options *Options) {
		options.AIProviderScan = func() []aiproviderimport.SourceInfo {
			return []aiproviderimport.SourceInfo{importSource(aiproviderimport.ToolCCSwitch, "/tmp/cc-switch.db",
				importCandidate("No Vault", store.KindOpenAI, "https://novault.example.com/v1", "secret-novault", "m1"))}
		}
	})
	state := world.state
	state.secrets.vault = nil // the key cannot be stored anywhere
	importOpen(t, world)
	importPreview(t, world, 0)
	importApply(t, world)

	if len(state.ai.providers) != 1 || state.ai.providers[0].HasKey {
		t.Fatalf("providers = %#v", state.ai.providers)
	}
	result := state.ai.cfg.importResult
	if result.Created != 1 || result.KeyFailed != 1 {
		t.Fatalf("result = %#v", result)
	}
	_, texts := world.paint()
	if !contains(texts, fmt.Sprintf(state.messages.AISettings.ImportKeyFailedFmt, 1)) {
		t.Fatalf("the key failure is not reported: %v", texts)
	}
	for _, text := range texts {
		if strings.Contains(text, "secret-novault") {
			t.Fatalf("a credential is painted: %q", text)
		}
	}
}
