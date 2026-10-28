package workbench

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/agent"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/i18n"
)

const testSecretKey = "sk-test-SECRET-0123456789"

// lister is a provider that only lists models. It records the key it was made
// with and can be held until released.
type lister struct {
	mu        sync.Mutex
	names     []string
	err       error
	hold      chan struct{} // when set, Models waits for it or for cancellation
	calls     int
	key       string
	cancelled chan struct{}
}

func newLister(names ...string) *lister {
	return &lister{names: names, cancelled: make(chan struct{}, 4)}
}

func (l *lister) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	return nil, errors.New("not used")
}

func (l *lister) Models(ctx context.Context) ([]string, error) {
	l.mu.Lock()
	l.calls++
	hold, names, err := l.hold, l.names, l.err
	l.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			l.cancelled <- struct{}{}
			return nil, ctx.Err()
		}
	}
	return names, err
}

// settingsWorld is a window on the Settings page with the assistant pointed at
// a temporary database and a fake provider.
type settingsWorld struct {
	state   *State
	options Options
	lister  *lister
	changed int
	clip    []string
}

func newSettingsWorld(t *testing.T, fake *lister) *settingsWorld {
	t.Helper()
	return newSettingsWorldWith(t, fake, nil)
}

// newSettingsWorldWith is newSettingsWorld with the options adjusted before the
// window is created, which is where a test installs a seam.
func newSettingsWorldWith(t *testing.T, fake *lister, adjust func(*Options)) *settingsWorld {
	t.Helper()
	world := &settingsWorld{lister: fake}
	world.options = freshOptions(t)
	world.options.AIStorePath = filepath.Join(t.TempDir(), "ai.db")
	world.options.Invalidate = func() {}
	world.options.Keystore = &memKeys{} // never the real keychain
	world.options.AISettingsChanged = func() { world.changed++ }
	world.options.SetClipboardText = func(text string) error {
		world.clip = append(world.clip, text)
		return nil
	}
	world.options.AIProvider = func(provider store.Provider) llm.Provider {
		fake.mu.Lock()
		fake.key = provider.APIKey
		fake.mu.Unlock()
		return fake
	}
	if adjust != nil {
		adjust(&world.options)
	}
	world.state = world.open(t)
	return world
}

// open makes a window on the same database.
func (world *settingsWorld) open(t *testing.T) *State {
	t.Helper()
	state := New(world.options)
	t.Cleanup(state.Close)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAssistant)
	return state
}

func (world *settingsWorld) paint() (*ui.Canvas, []string) {
	var canvas ui.Canvas
	world.state.Paint(&canvas, ui.Size{Width: 1400, Height: 3200})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return &canvas, texts
}

// type puts text into the field behind a hit the way the runtime does.
func (world *settingsWorld) typeInto(t *testing.T, hit ui.ID, value string) {
	t.Helper()
	editor := world.state.EditorFor(hit)
	if editor == nil {
		t.Fatalf("hit %d has no editor", hit)
	}
	editor.Insert(value)
}

// addOpenAI walks through the dialog and returns the stored provider.
func (world *settingsWorld) addOpenAI(t *testing.T, name, baseURL, key string) store.Provider {
	t.Helper()
	state := world.state
	state.Click(hitAISetProviderAdd)
	if state.ai.cfg.dialog != aiDialogProvider {
		t.Fatal("the provider dialog did not open")
	}
	world.typeInto(t, hitAISetFormName, name)
	world.typeInto(t, hitAISetFormURL, baseURL)
	world.typeInto(t, hitAISetFormKey, key)
	state.Click(hitAISetFormSubmit)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatalf("the dialog stayed open: %q", state.ai.cfg.formErr)
	}
	state.cancelAIFetch() // saving lists the models by itself; these tests press the button
	for _, provider := range state.ai.providers {
		if provider.Name == name {
			return provider
		}
	}
	t.Fatalf("provider %q was not stored: %v", name, state.ai.providers)
	return store.Provider{}
}

func (world *settingsWorld) db(t *testing.T) *store.Store {
	t.Helper()
	db, err := world.state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func settingValue(t *testing.T, db *store.Store, key string) string {
	t.Helper()
	value, _, err := db.Setting(key)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAIProviderIsAddedStoredAndItsKeyNeverPainted(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.Click(hitAISetProviderAdd)
	world.typeInto(t, hitAISetFormName, "Work")
	state.Click(hitAISetFormKindClaude)
	world.typeInto(t, hitAISetFormKey, testSecretKey)

	// While the key is typed the dialog shows bullets, and the hit holds no value.
	canvas, texts := world.paint()
	for _, text := range texts {
		if strings.Contains(text, testSecretKey) {
			t.Fatalf("the key is painted: %q", text)
		}
	}
	hit, ok := hitByID(canvas, hitAISetFormKey)
	if !ok || !hit.Secure || hit.Value != "" {
		t.Fatalf("key field hit = %+v, ok=%v, want a secure hit with no value", hit, ok)
	}
	if !contains(texts, state.messages.AISettings.FormBaseURLClaude) {
		t.Fatalf("the Anthropic default URL is not the placeholder: %v", texts)
	}

	state.Click(hitAISetFormSubmit)
	if state.ai.cfg.dialog != aiDialogNone || len(state.ai.providers) != 1 {
		t.Fatalf("dialog=%d providers=%v err=%q", state.ai.cfg.dialog, state.ai.providers, state.ai.cfg.formErr)
	}
	saved := state.ai.providers[0]
	if saved.Name != "Work" || saved.Kind != store.KindAnthropic || state.aiKeyOf(saved) != testSecretKey || !saved.Enabled || saved.BaseURL != "" {
		t.Fatalf("stored provider = %#v", saved)
	}
	if world.changed != 1 {
		t.Fatalf("the application was told %d times, want 1", world.changed)
	}
	// The key left the editor, and it is on disk.
	if state.ai.cfg.apiKey.String() != "" {
		t.Fatal("the key is still in the editor")
	}
	fromDisk, err := world.db(t).Providers()
	if err != nil || len(fromDisk) != 1 || !fromDisk[0].HasKey || fromDisk[0].APIKey != "" {
		t.Fatalf("providers on disk = %v err=%v", fromDisk, err)
	}
	if key, found, err := state.secrets.vault.Get(aiKeySecretName(saved.ID)); err != nil || !found || key != testSecretKey {
		t.Fatalf("the vault holds %q found=%v err=%v", key, found, err)
	}
	_, texts = world.paint()
	for _, text := range texts {
		if strings.Contains(text, testSecretKey) {
			t.Fatalf("the saved key is painted: %q", text)
		}
	}
	if !contains(texts, "Work") || !contains(texts, state.messages.AISettings.ProviderKeySaved) {
		t.Fatalf("the provider row is missing: %v", texts)
	}
	// A new window sees it too.
	again := world.open(t)
	again.Wake()
	if len(again.ai.providers) != 1 {
		t.Fatalf("a new window has providers %v", again.ai.providers)
	}
}

func TestAIProviderAuthStyleChoiceKeepsTheSavedKey(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	m := state.messages.AISettings
	state.Click(hitAISetProviderAdd)
	world.typeInto(t, hitAISetFormName, "Relay")
	state.Click(hitAISetFormKindClaude)
	state.Click(hitAISetFormAuthBearer)
	world.typeInto(t, hitAISetFormKey, testSecretKey)
	state.Click(hitAISetFormSubmit)
	if len(state.ai.providers) != 1 {
		t.Fatalf("providers = %v", state.ai.providers)
	}
	provider := state.ai.providers[0]
	if provider.AuthStyle != store.AuthStyleBearer || state.aiKeyOf(provider) != testSecretKey {
		t.Fatalf("stored provider = %#v, key %q", provider, state.aiKeyOf(provider))
	}
	_, texts := world.paint()
	if !contains(texts, m.ProviderAuthBearer) {
		t.Fatalf("the provider row does not show the bearer style: %v", texts)
	}
	for _, value := range texts {
		if strings.Contains(value, testSecretKey) {
			t.Fatalf("the saved key is painted: %q", value)
		}
	}

	state.Click(hitAISetProviderEdit.At(0))
	if state.ai.cfg.authStyle != store.AuthStyleBearer {
		t.Fatalf("edit form style = %q", state.ai.cfg.authStyle)
	}
	canvas, texts := world.paint()
	if _, ok := hitByID(canvas, hitAISetFormAuthBearer); !ok {
		t.Fatalf("the edit form has no bearer choice: %v", texts)
	}
	for _, value := range texts {
		if strings.Contains(value, testSecretKey) {
			t.Fatalf("the saved key is painted in the edit form: %q", value)
		}
	}
	// Switching the header does not require re-entering the credential.
	state.Click(hitAISetFormAuthAPIKey)
	state.Click(hitAISetFormSubmit)
	provider = state.ai.providers[0]
	if provider.AuthStyle != store.AuthStyleAPIKey || state.aiKeyOf(provider) != testSecretKey {
		t.Fatalf("after switching style: %#v, key %q", provider, state.aiKeyOf(provider))
	}

	// An OpenAI-compatible provider has no choice of header.
	state.Click(hitAISetProviderAdd)
	canvas, _ = world.paint()
	if _, ok := hitByID(canvas, hitAISetFormAuthBearer); ok {
		t.Fatal("the OpenAI form offers a bearer/API-key choice")
	}
}

func TestAIProviderPassesTheAuthenticationStyleToTheRequest(t *testing.T) {
	var header http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		w.Write([]byte(`{"data":[{"id":"m"}],"has_more":false}`))
	}))
	defer srv.Close()

	options := freshOptions(t)
	options.Keystore = &memKeys{}
	options.AIProvider = nil // exercise the real wire client
	state := newTestState(t, options)
	for _, test := range []struct {
		style  string
		apiKey string
		bearer string
	}{
		{store.AuthStyleAPIKey, "key", ""},
		{store.AuthStyleBearer, "", "Bearer key"},
	} {
		header = nil
		provider := state.aiProvider(store.Provider{
			Kind: store.KindAnthropic, BaseURL: srv.URL, APIKey: "key", AuthStyle: test.style,
		})
		if _, err := provider.Models(context.Background()); err != nil {
			t.Fatal(err)
		}
		if header.Get("x-api-key") != test.apiKey || header.Get("Authorization") != test.bearer {
			t.Fatalf("style %q sent %v", test.style, header)
		}
	}
}

func TestAIProviderFormValidates(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	m := state.messages.AISettings
	state.Click(hitAISetProviderAdd)
	state.Click(hitAISetFormSubmit)
	if state.ai.cfg.formErr != m.ErrFormName || state.ai.cfg.dialog != aiDialogProvider {
		t.Fatalf("empty name: err=%q dialog=%d", state.ai.cfg.formErr, state.ai.cfg.dialog)
	}
	world.typeInto(t, hitAISetFormName, "x")
	world.typeInto(t, hitAISetFormURL, "ftp://host")
	state.Click(hitAISetFormSubmit)
	if state.ai.cfg.formErr != m.ErrFormBaseURL {
		t.Fatalf("bad URL: err=%q", state.ai.cfg.formErr)
	}
	for range "ftp://host" {
		state.EditorFor(hitAISetFormURL).DeleteBackward()
	}
	state.Click(hitAISetFormSubmit)
	if state.ai.cfg.formErr != m.ErrFormKey {
		t.Fatalf("official endpoint without key: err=%q", state.ai.cfg.formErr)
	}
	_, texts := world.paint()
	if !contains(texts, m.ErrFormKey) {
		t.Fatalf("the error is not painted: %v", texts)
	}
	if len(state.ai.providers) != 0 {
		t.Fatal("an invalid form stored a provider")
	}
	// A local server needs no key.
	world.typeInto(t, hitAISetFormURL, "http://127.0.0.1:11434/v1")
	state.Click(hitAISetFormSubmit)
	if len(state.ai.providers) != 1 || state.ai.providers[0].APIKey != "" {
		t.Fatalf("providers = %v", state.ai.providers)
	}
	// Cancel stores nothing and forgets what was typed.
	state.Click(hitAISetProviderAdd)
	world.typeInto(t, hitAISetFormName, "gone")
	world.typeInto(t, hitAISetFormKey, testSecretKey)
	state.Click(hitAISetFormCancel)
	if state.ai.cfg.dialog != aiDialogNone || state.ai.cfg.name.String() != "" || state.ai.cfg.apiKey.String() != "" || len(state.ai.providers) != 1 {
		t.Fatalf("cancel left dialog=%d name=%q providers=%d", state.ai.cfg.dialog, state.ai.cfg.name.String(), len(state.ai.providers))
	}
}

func TestAIEditKeepsTheOldKeyUnlessReplaced(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Work", "", testSecretKey)

	state.Click(hitAISetProviderEdit.At(0))
	if state.ai.cfg.editID != provider.ID || !state.ai.cfg.hadKey || state.ai.cfg.apiKey.String() != "" {
		t.Fatalf("edit form: id=%q hadKey=%v key=%q", state.ai.cfg.editID, state.ai.cfg.hadKey, state.ai.cfg.apiKey.String())
	}
	if state.ai.cfg.name.String() != "Work" {
		t.Fatalf("the name field holds %q", state.ai.cfg.name.String())
	}
	_, texts := world.paint()
	if !contains(texts, state.messages.AISettings.FormKeySaved) {
		t.Fatalf("the saved-key hint is missing: %v", texts)
	}
	for _, text := range texts {
		if strings.Contains(text, testSecretKey) {
			t.Fatalf("the saved key is painted in the edit dialog: %q", text)
		}
	}
	world.typeInto(t, hitAISetFormName, " B")
	state.Click(hitAISetFormKindClaude)
	state.Click(hitAISetFormSubmit)
	got := state.ai.providers[0]
	if got.ID != provider.ID || got.Name != "Work B" || got.Kind != store.KindAnthropic || state.aiKeyOf(got) != testSecretKey {
		t.Fatalf("after editing with the key left empty: %#v", got)
	}

	state.Click(hitAISetProviderEdit.At(0))
	world.typeInto(t, hitAISetFormKey, "sk-new-key-9999")
	state.Click(hitAISetFormSubmit)
	if got := state.ai.providers[0]; state.aiKeyOf(got) != "sk-new-key-9999" || got.ID != provider.ID || len(state.ai.providers) != 1 {
		t.Fatalf("after replacing the key: %#v (%d providers)", got, len(state.ai.providers))
	}
}

func TestAIProviderSwitchAndDelete(t *testing.T) {
	world := newSettingsWorld(t, newLister("m1"))
	state := world.state
	first := world.addOpenAI(t, "A", "", "k1")
	second := world.addOpenAI(t, "B", "", "k2")
	db := world.db(t)
	for _, provider := range []store.Provider{first, second} {
		if err := db.SaveModels(provider.ID, []store.Model{{ProviderID: provider.ID, Name: "m", Enabled: true}}); err != nil {
			t.Fatal(err)
		}
	}
	state.reloadAI()

	state.Click(hitAISetProviderSwitch.At(0)) // A, the first by name
	if state.ai.providers[0].Enabled || state.aiKeyOf(state.ai.providers[0]) != "k1" {
		t.Fatalf("after the switch: %#v", state.ai.providers[0])
	}

	// Cancelling the question keeps the provider.
	state.Click(hitAISetProviderDelete.At(0))
	if state.ai.cfg.dialog != aiDialogDelete {
		t.Fatal("no delete question")
	}
	_, texts := world.paint()
	if !contains(texts, state.messages.AISettings.DeleteTitle) || !contains(texts, `"A"`) {
		t.Fatalf("the question is not painted: %v", texts)
	}
	state.Click(hitAISetConfirmCancel)
	if len(state.ai.providers) != 2 {
		t.Fatalf("cancel deleted: %v", state.ai.providers)
	}
	state.Click(hitAISetProviderDelete.At(0))
	state.Click(hitAISetConfirmAction)
	if len(state.ai.providers) != 1 || state.ai.providers[0].ID != second.ID {
		t.Fatalf("providers after delete = %v", state.ai.providers)
	}
	models, _ := db.Models()
	if len(models) != 1 || models[0].ProviderID != second.ID {
		t.Fatalf("models after the delete = %v, want only the other provider's", models)
	}
}

func TestAIFetchModelsListsThemAndKeepsChoices(t *testing.T) {
	fake := newLister("gpt-b", "gpt-a", "gpt-a", " ")
	fake.hold = make(chan struct{})
	world := newSettingsWorld(t, fake)
	state := world.state
	provider := world.addOpenAI(t, "Work", "", testSecretKey)

	state.Click(hitAISetFetchModels)
	if state.ai.cfg.fetch == nil {
		t.Fatal("no fetch started")
	}
	_, texts := world.paint()
	if !contains(texts, state.messages.AISettings.ModelsFetching) || !contains(texts, state.messages.AISettings.ModelsCancel) {
		t.Fatalf("progress is not shown: %v", texts)
	}
	close(fake.hold)
	pumpSettings(t, state, "the fetch", func() bool { return state.ai.cfg.fetch == nil })
	if fake.key != testSecretKey {
		t.Fatalf("the provider client was made with key %q", fake.key)
	}
	models := state.aiModelList(provider.ID)
	if len(models) != 2 || models[0].Name != "gpt-a" || models[1].Name != "gpt-b" || models[0].Enabled {
		t.Fatalf("models = %v, want gpt-a and gpt-b, disabled", models)
	}
	_, texts = world.paint()
	if !contains(texts, "gpt-a") || !contains(texts, "2 models found.") {
		t.Fatalf("the models are not listed: %v", texts)
	}

	// Enable one, choose the other as default, fetch again: choices stay.
	state.Click(hitAISetModelSwitch.At(0)) // gpt-a
	state.Click(hitAISetModelUse.At(1))
	fake.mu.Lock()
	fake.hold, fake.names = nil, []string{"gpt-b", "gpt-c"}
	fake.mu.Unlock()
	state.Click(hitAISetFetchModels)
	pumpSettings(t, state, "the second fetch", func() bool { return state.ai.cfg.fetch == nil })
	got := map[string]bool{}
	for _, model := range state.aiModelList(provider.ID) {
		got[model.Name] = model.Enabled
	}
	if len(got) != 3 || !got["gpt-a"] || !got["gpt-b"] || got["gpt-c"] {
		t.Fatalf("models after refetch = %v, want a and b enabled, c not", got)
	}
}

func TestAIFetchModelsFailureShowsAScrubbedError(t *testing.T) {
	fake := newLister()
	fake.err = errors.New("401 for key " + testSecretKey)
	world := newSettingsWorld(t, fake)
	state := world.state
	world.addOpenAI(t, "Work", "", testSecretKey)
	state.Click(hitAISetFetchModels)
	pumpSettings(t, state, "the fetch", func() bool { return state.ai.cfg.fetch == nil })
	_, texts := world.paint()
	if !contains(texts, "Could not fetch models: 401 for key ***") {
		t.Fatalf("the error is not shown scrubbed: %v", texts)
	}
	for _, text := range texts {
		if strings.Contains(text, testSecretKey) {
			t.Fatalf("the key leaked into %q", text)
		}
	}
}

func TestAIFetchModelsCanBeCancelledAndEndsWithTheWindow(t *testing.T) {
	fake := newLister("m")
	fake.hold = make(chan struct{})
	world := newSettingsWorld(t, fake)
	state := world.state
	world.addOpenAI(t, "Work", "", "k")

	state.Click(hitAISetFetchModels)
	state.Click(hitAISetFetchModels) // the button now says Cancel
	if state.ai.cfg.fetch != nil {
		t.Fatal("the fetch is still pending after cancel")
	}
	select {
	case <-fake.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the request was not cancelled")
	}
	state.Wake()
	if models, _ := world.db(t).Models(); len(models) != 0 || state.ai.cfg.fetched {
		t.Fatalf("a cancelled fetch changed things: %v fetched=%v", models, state.ai.cfg.fetched)
	}

	state.Click(hitAISetFetchModels)
	state.Close()
	select {
	case <-fake.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the window did not cancel the request")
	}
}

func TestAIFetchModelsOutlivedByADeletedProviderStoresNothing(t *testing.T) {
	fake := newLister("m")
	fake.hold = make(chan struct{})
	world := newSettingsWorld(t, fake)
	state := world.state
	world.addOpenAI(t, "Work", "", "k")
	state.Click(hitAISetFetchModels)
	state.Click(hitAISetProviderDelete.At(0))
	state.Click(hitAISetConfirmAction)
	if state.ai.cfg.fetch != nil || len(state.ai.providers) != 0 {
		t.Fatalf("fetch=%v providers=%v", state.ai.cfg.fetch, state.ai.providers)
	}
	select {
	case <-fake.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("deleting the provider did not cancel the request")
	}
	state.Wake()
	if models, _ := world.db(t).Models(); len(models) != 0 {
		t.Fatalf("models = %v", models)
	}
}

func TestAIDefaultModelIsWhatTheEngineUses(t *testing.T) {
	fake := newLister("big", "small")
	world := newSettingsWorld(t, fake)
	state := world.state
	provider := world.addOpenAI(t, "Work", "", "k")
	if _, _, err := state.aiModel(); !errors.Is(err, errNoAIModel) {
		t.Fatalf("aiModel before any choice: %v", err)
	}
	state.Click(hitAISetFetchModels)
	pumpSettings(t, state, "the fetch", func() bool { return state.ai.cfg.fetch == nil })
	if _, model, err := state.aiModel(); err != nil || model != "big" {
		t.Fatalf("with no default, the first model listed becomes it: %q %v", model, err)
	}

	// "small" is the second row. Using it enables it and makes it the default.
	state.Click(hitAISetModelUse.At(1))
	chosen, model, err := state.aiModel()
	if err != nil || model != "small" || chosen.ID != provider.ID {
		t.Fatalf("aiModel = %v %q %v", chosen, model, err)
	}
	if enabled := state.aiModelList(provider.ID)[1].Enabled; !enabled {
		t.Fatal("the default model is not enabled")
	}
	// The default's switch cannot turn it off; the other model's can.
	state.Click(hitAISetModelSwitch.At(1))
	if !state.aiModelList(provider.ID)[1].Enabled {
		t.Fatal("the default model was disabled")
	}
	state.Click(hitAISetModelSwitch.At(0))
	if state.aiModelList(provider.ID)[0].Enabled {
		t.Fatal("big, no longer the default, should switch off")
	}
	canvas, texts := world.paint()
	if !contains(texts, state.messages.AISettings.ModelDefault) {
		t.Fatalf("the default is not marked: %v", texts)
	}
	if hit, ok := hitByID(canvas, hitAISetModelSwitch.At(1)); !ok || !hit.Disabled || !hit.Checked {
		t.Fatalf("the default model's switch = %+v", hit)
	}

	// Switching the provider off takes the model away.
	state.Click(hitAISetProviderSwitch.At(0))
	if _, _, err := state.aiModel(); !errors.Is(err, errNoAIModel) {
		t.Fatalf("aiModel with the provider off: %v", err)
	}
}

func TestAIModelsCanBeAddedByNameAndPaged(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	provider := world.addOpenAI(t, "Local", "http://127.0.0.1:11434/v1", "")

	world.typeInto(t, hitAISetModelInput, "llama 3")
	state.Click(hitAISetModelAdd)
	if state.ai.cfg.modelErr != "" {
		t.Fatalf("a model name has no spaces, but the editor accepts them: %q", state.ai.cfg.modelErr)
	}
	if got := state.aiModelList(provider.ID); len(got) != 1 || got[0].Name != "llama3" || !got[0].Enabled {
		t.Fatalf("models = %v", got)
	}
	// The first model becomes the default when there was none.
	if _, model, err := state.aiModel(); err != nil || model != "llama3" {
		t.Fatalf("aiModel = %q %v", model, err)
	}
	if state.ai.cfg.modelInput.String() != "" {
		t.Fatal("the field was not cleared")
	}
	for index := 0; index < 11; index++ {
		world.typeInto(t, hitAISetModelInput, "model-"+string(rune('a'+index)))
		state.Click(hitAISetModelAdd)
	}
	_, texts := world.paint()
	if !contains(texts, "1-8 of 12") {
		t.Fatalf("no pager: %v", texts)
	}
	state.Click(hitAISetModelNext)
	_, texts = world.paint()
	if !contains(texts, "9-12 of 12") {
		t.Fatalf("second page: %v", texts)
	}
	state.Click(hitAISetModelNext)
	if state.ai.cfg.page != 1 {
		t.Fatalf("the page ran past the end: %d", state.ai.cfg.page)
	}
	state.Click(hitAISetModelPrev)
	if state.ai.cfg.page != 0 {
		t.Fatalf("page = %d", state.ai.cfg.page)
	}
}

func TestAIBehaviourChoicesPersistAndSurviveANewWindow(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	for _, hit := range []ui.ID{hitAISetRiskHigh, hitAISetSteps24, hitAISetContext120} {
		state.Click(hit)
	}
	state.Click(hitAISetRedact)
	state.Click(hitAISetHistory)
	db := world.db(t)
	for key, want := range map[string]string{
		"ai.auto_up_to": "high", "ai.max_steps": "24", "ai.context_lines": "120", "ai.redact": "off", "ai.history": "off",
	} {
		if got := settingValue(t, db, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	want := aiSettings{autoUpTo: risk.High, maxSteps: 24, contextLines: 120, redact: false, keepHistory: false, mcpMode: gate.ModeObserve, agentMode: gate.ModeObserve}
	if state.ai.settings != want {
		t.Fatalf("settings = %+v, want %+v", state.ai.settings, want)
	}
	if world.changed != 5 {
		t.Fatalf("the application was told %d times, want 5", world.changed)
	}
	state.Close()
	again := world.open(t)
	if again.ai.settings != want {
		t.Fatalf("a new window has %+v, want %+v", again.ai.settings, want)
	}
	canvas, _ := paintedSettings(again)
	for _, id := range []ui.ID{hitAISetRedact, hitAISetHistory} {
		if hit, ok := hitByID(canvas, id); !ok || hit.Checked {
			t.Errorf("switch %d should be off: %+v", id, hit)
		}
	}
	// And back.
	again.Click(hitAISetRiskLow)
	again.Click(hitAISetSteps6)
	again.Click(hitAISetContext0)
	again.Click(hitAISetRedact)
	again.Click(hitAISetHistory)
	if got, want := again.ai.settings, (aiSettings{autoUpTo: risk.Low, maxSteps: 6, contextLines: 0, redact: true, keepHistory: true, mcpMode: gate.ModeObserve, agentMode: gate.ModeObserve}); got != want {
		t.Fatalf("settings = %+v, want %+v", got, want)
	}
}

func paintedSettings(state *State) (*ui.Canvas, []string) {
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 3200})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return &canvas, texts
}

func TestAIClearHistoryAsksFirst(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	db := world.db(t)
	conversation, err := db.CreateConversation("hello", "")
	if err != nil {
		t.Fatal(err)
	}
	state.ai.conv = conversation.ID
	state.ai.items = []aiItem{{kind: aiUser, text: "hello"}}
	state.ai.history = []llm.Message{{Role: llm.RoleUser, Text: "hello"}}

	state.Click(hitAISetClearHistory)
	_, texts := world.paint()
	if state.ai.cfg.dialog != aiDialogClear || !contains(texts, state.messages.AISettings.ClearTitle) {
		t.Fatalf("no confirmation: dialog=%d texts=%v", state.ai.cfg.dialog, texts)
	}
	state.Click(hitAISetConfirmCancel)
	if list, _ := db.Conversations("", 0); len(list) != 1 || len(state.ai.items) != 1 {
		t.Fatalf("cancel changed things: %v items=%d", list, len(state.ai.items))
	}

	state.Click(hitAISetClearHistory)
	state.Click(hitAISetConfirmAction)
	if list, _ := db.Conversations("", 0); len(list) != 0 {
		t.Fatalf("conversations after clearing: %v", list)
	}
	if len(state.ai.items) != 0 || len(state.ai.history) != 0 || state.ai.conv != 0 {
		t.Fatalf("the panel was not reset: items=%d history=%d conv=%d", len(state.ai.items), len(state.ai.history), state.ai.conv)
	}
	_, texts = world.paint()
	if !contains(texts, state.messages.AISettings.ClearHistoryDone) {
		t.Fatalf("no confirmation text: %v", texts)
	}
	// While a request runs the button is inert.
	state.ai.run = &aiRun{cancel: func() {}}
	state.Click(hitAISetClearHistory)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatal("the question opened during a request")
	}
	state.ai.run = nil
}

func TestAIExternalAccessSettingsPersistAndCopy(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsMCP)
	db := world.db(t)
	_, texts := world.paint()
	if !contains(texts, state.messages.AISettings.MCPOff) {
		t.Fatalf("the endpoint should say it is off: %v", texts)
	}
	state.Click(hitAISetMCPEnabled)
	state.Click(hitAISetMCPAsk)
	if settingValue(t, db, "ai.mcp.enabled") != "on" || settingValue(t, db, "ai.mcp.mode") != "ask" {
		t.Fatal("the MCP choices are not stored")
	}
	if !state.ai.settings.mcpEnabled || state.ai.settings.mcpMode != gate.ModeAsk || world.changed != 2 {
		t.Fatalf("settings=%+v changed=%d", state.ai.settings, world.changed)
	}
	_, texts = world.paint()
	if !contains(texts, state.messages.AISettings.MCPNotRunning) {
		t.Fatalf("not running is not said: %v", texts)
	}
	// Nothing to copy until the server runs.
	canvas, _ := world.paint()
	if hit, ok := hitByID(canvas, hitAISetMCPCopy); !ok || !hit.Disabled {
		t.Fatalf("copy should be disabled: %+v", hit)
	}
	state.Click(hitAISetMCPCopy)
	if len(world.clip) != 0 {
		t.Fatalf("copied %v with no server", world.clip)
	}

	state.ai.mcpAddr, state.ai.mcpToken = "127.0.0.1:4455", "tok-"+strings.Repeat("x", 30)
	canvas, texts = world.paint()
	if !contains(texts, "http://127.0.0.1:4455/mcp") {
		t.Fatalf("the endpoint is not shown: %v", texts)
	}
	for _, text := range texts {
		if strings.Contains(text, state.ai.mcpToken) {
			t.Fatalf("the token is painted: %q", text)
		}
	}
	if hit, ok := hitByID(canvas, hitAISetMCPCopy); !ok || hit.Disabled {
		t.Fatalf("copy should be enabled: %+v", hit)
	}
	state.Click(hitAISetMCPCopy)
	want := `claude mcp add --transport http motty http://127.0.0.1:4455/mcp --header "Authorization: Bearer ` + state.ai.mcpToken + `"`
	if len(world.clip) != 1 || world.clip[0] != want {
		t.Fatalf("clipboard = %q, want %q", world.clip, want)
	}
	_, texts = world.paint()
	if !contains(texts, state.messages.AISettings.MCPCopied) {
		t.Fatalf("no confirmation: %v", texts)
	}

	// Resetting the token clears it, so the application makes a new one.
	if err := db.SetSetting("ai.mcp.token", "old-token"); err != nil {
		t.Fatal(err)
	}
	before := world.changed
	state.Click(hitAISetMCPReset)
	if settingValue(t, db, "ai.mcp.token") != "" || world.changed != before+1 {
		t.Fatalf("token=%q changed=%d", settingValue(t, db, "ai.mcp.token"), world.changed)
	}
	state.Click(hitAISetMCPObserve)
	if settingValue(t, db, "ai.mcp.mode") != "observe" || state.ai.settings.mcpMode != gate.ModeObserve {
		t.Fatal("observe was not stored")
	}
	state.Close()
	if again := world.open(t); !again.ai.settings.mcpEnabled || again.ai.settings.mcpMode != gate.ModeObserve {
		t.Fatalf("a new window has %+v", again.ai.settings)
	}
}

func TestAISettingsPaintInBothLanguages(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		world := newSettingsWorld(t, newLister("m"))
		state := world.state
		state.setLanguage(language)
		world.addOpenAI(t, "Work", "", "k")
		state.Click(hitAISetFetchModels)
		pumpSettings(t, state, "the fetch", func() bool { return state.ai.cfg.fetch == nil })

		check := func(what string) {
			canvas, texts := world.paint()
			for _, text := range canvas.Texts() {
				if strings.TrimSpace(text.Value) == "" {
					t.Errorf("%v %s: an empty label at %+v", language, what, text.Bounds)
				}
			}
			for _, hit := range canvas.Hits() {
				if tree.Within(hit.ID.Scope(), aiSettingsHits) && hit.Label == "" && hit.Role != 0 {
					t.Errorf("%v %s: hit %d has no accessible name", language, what, hit.ID)
				}
			}
			_ = texts
		}
		check("page")
		if want := state.messages.AISettings.Title; language == i18n.Chinese && want != "AI 助手" {
			t.Fatalf("title = %q", want)
		}
		state.Click(hitAISetProviderEdit.At(0))
		check("provider dialog")
		state.Click(hitAISetFormCancel)
		state.Click(hitAISetProviderDelete.At(0))
		check("delete question")
		state.Click(hitAISetConfirmCancel)
		state.Click(hitAISetClearHistory)
		check("clear question")
		state.Click(hitAISetConfirmCancel)
		state.Close()
	}
}

func TestAISettingsFocusOrderFollowsThePage(t *testing.T) {
	world := newSettingsWorld(t, newLister("text-embedding-m")) // not for chat, so it does not become the default and stays switchable
	state := world.state
	world.addOpenAI(t, "Work", "", "k")
	state.Click(hitAISetFetchModels)
	pumpSettings(t, state, "the fetch", func() bool { return state.ai.cfg.fetch == nil })
	canvas, _ := world.paint()
	var order []ui.ID
	for _, hit := range canvas.Hits() {
		if hit.Focusable && tree.Within(hit.ID.Scope(), aiSettingsHits) && !hit.Disabled {
			order = append(order, hit.ID)
		}
	}
	want := []ui.ID{
		hitAISetProviderRow.At(0), hitAISetProviderSwitch.At(0), hitAISetProviderEdit.At(0), hitAISetProviderDelete.At(0), hitAISetProviderAdd,
		hitAISetFetchModels, hitAISetModelUse.At(0), hitAISetModelSwitch.At(0), hitAISetModelInput, hitAISetModelAdd,
		hitAISetRiskLow, hitAISetSteps12, hitAISetContext60, hitAISetRedact, hitAISetHistory, hitAISetClearHistory,
	}
	position := -1
	for _, id := range want {
		found := -1
		for index, got := range order {
			if got == id {
				found = index
			}
		}
		if found < 0 {
			// A segmented control is one stop at its selected segment; the
			// unselected ones may not be focusable.
			t.Errorf("hit %d is not a focus stop; stops: %v", id, order)
			continue
		}
		if found <= position {
			t.Errorf("hit %d comes out of order in %v", id, order)
		}
		position = found
	}

	state.selectSettingsCategory(settingsMCP)
	canvas, _ = world.paint()
	order = order[:0]
	for _, hit := range canvas.Hits() {
		if hit.Focusable && tree.Within(hit.ID.Scope(), aiSettingsHits) && !hit.Disabled {
			order = append(order, hit.ID)
		}
	}
	next := 0
	for _, got := range order {
		if next < 3 && got == []ui.ID{hitAISetMCPEnabled, hitAISetMCPObserve, hitAISetMCPReset}[next] {
			next++
		}
	}
	if next != 3 {
		t.Fatalf("the external access page's focus stops are %v", order)
	}
}

func TestSettingsPageScrollsToTheAssistantSection(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.selectSettingsCategory(settingsAssistant)
	var canvas ui.Canvas
	size := ui.Size{Width: 1200, Height: 700}
	state.Paint(&canvas, size)
	if state.settingsContent <= size.Height {
		t.Fatalf("the page (%v) is not taller than the window", state.settingsContent)
	}
	for range 80 {
		state.scrollSettings(ui.ScrollEvent{DeltaY: 200, Precise: true})
	}
	canvas = ui.Canvas{}
	state.Paint(&canvas, size)
	var visible bool
	for _, text := range canvas.Texts() {
		if text.Value == state.messages.AISettings.BehaviourTitle && text.Bounds.Y > 0 && text.Bounds.Y < size.Height {
			visible = true
		}
	}
	if !visible {
		t.Fatalf("the last group is not reachable by scrolling (scroll %v of %v)", state.settingsScroll, state.settingsContent)
	}
}

func TestAISettingsDialogBlocksThePageBehindIt(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.Click(hitAISetProviderAdd)
	before := state.language
	state.Click(hitSettingsLanguageChinese)
	if state.language != before {
		t.Fatal("a click reached the page behind the dialog")
	}
	state.Click(hitAISetFormDismiss)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatal("dismiss did not close the dialog")
	}
	state.Click(hitSettingsLanguageChinese)
	if state.language != i18n.Chinese {
		t.Fatal("the page does not work after the dialog")
	}
}

func TestAIStoreFailureIsShownNotHidden(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	// A file where a directory is needed makes the open fail.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	world.options.AIStorePath = filepath.Join(blocker, "ai.db")
	state := world.open(t)
	world.state = state
	_, texts := world.paint()
	if !contains(texts, "could not be opened") && !contains(texts, "无法打开") {
		t.Fatalf("the failure is not shown: %v", texts)
	}
	state.Click(hitAISetProviderAdd)
	if state.ai.cfg.dialog != aiDialogNone {
		t.Fatal("the dialog opened with no database")
	}
}

// pumpSettings runs the window thread's wake-ups until the condition holds.
func pumpSettings(t *testing.T, state *State, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		state.Wake()
		time.Sleep(2 * time.Millisecond)
	}
}

func TestAISavingAProviderListsItsModelsAndPicksAUsableDefault(t *testing.T) {
	fake := newLister("text-embedding-3", "chat-a", "chat-b")
	world := newSettingsWorld(t, fake)
	state := world.state
	state.Click(hitAISetProviderAdd)
	world.typeInto(t, hitAISetFormName, "Work")
	world.typeInto(t, hitAISetFormKey, testSecretKey)
	state.Click(hitAISetFormSubmit)
	pumpSettings(t, state, "the automatic fetch", func() bool { return state.ai.cfg.fetch == nil && state.ai.cfg.fetched })

	provider, model, err := state.aiModel()
	if err != nil || model != "chat-a" || provider.Name != "Work" {
		t.Fatalf("assistant model after saving = %q %v, want chat-a", model, err)
	}
	if total, enabled := state.aiModelsOf(provider.ID); total != 3 || enabled != 1 {
		t.Fatalf("models = %d, enabled = %d, want 3 and only the default", total, enabled)
	}

	// Saving again with nothing that changes how the provider is reached lists nothing.
	calls := fake.calls
	state.Click(hitAISetProviderEdit.At(0))
	world.typeInto(t, hitAISetFormName, " 2")
	state.Click(hitAISetFormSubmit)
	if fake.calls != calls || state.ai.cfg.fetch != nil {
		t.Fatalf("a rename fetched models again (%d -> %d calls)", calls, fake.calls)
	}

	// A new key does.
	state.Click(hitAISetProviderEdit.At(0))
	world.typeInto(t, hitAISetFormKey, "sk-another-key-123")
	state.Click(hitAISetFormSubmit)
	pumpSettings(t, state, "the fetch for the new key", func() bool { return state.ai.cfg.fetch == nil && fake.calls == calls+1 })
}

func TestAProviderCanStartFromAPresetAndKeepsAnyNameTypedByHand(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.Click(hitAISetProviderAdd)
	world.paint()
	state.Click(hitAISetFormPreset)
	if !state.ai.cfg.presetOpen {
		t.Fatal("the list of presets did not open")
	}
	world.paint()
	deepseek := 3
	state.Click(hitAISetFormPresetItems.At(deepseek))
	cfg := &state.ai.cfg
	if cfg.presetOpen || cfg.baseURL.String() != aiPresets[deepseek].url || cfg.name.String() != "DeepSeek" || cfg.kind != store.KindOpenAI {
		t.Fatalf("form after choosing DeepSeek: open=%v url=%q name=%q kind=%q", cfg.presetOpen, cfg.baseURL.String(), cfg.name.String(), cfg.kind)
	}
	// Choosing another preset replaces what the first one put there, and Claude speaks the other API.
	state.Click(hitAISetFormPreset)
	state.Click(hitAISetFormPresetItems.At(0))
	if cfg.name.String() != "Claude" || cfg.kind != store.KindAnthropic || cfg.baseURL.String() != aiPresets[0].url {
		t.Fatalf("form after choosing Claude: url=%q name=%q kind=%q", cfg.baseURL.String(), cfg.name.String(), cfg.kind)
	}
	// A name the user typed is not overwritten.
	cfg.name.SetSelection(text.Range{Start: 0, End: cfg.name.RuneCount()})
	cfg.name.Insert("Work")
	state.Click(hitAISetFormPreset)
	state.Click(hitAISetFormPresetItems.At(1))
	if cfg.name.String() != "Work" || cfg.baseURL.String() != aiPresets[1].url {
		t.Fatalf("form after choosing OpenAI: url=%q name=%q", cfg.baseURL.String(), cfg.name.String())
	}
	world.typeInto(t, hitAISetFormKey, testSecretKey)
	state.Click(hitAISetFormSubmit)
	state.cancelAIFetch()
	var saved store.Provider
	for _, provider := range state.ai.providers {
		if provider.Name == "Work" {
			saved = provider
		}
	}
	if saved.BaseURL != aiPresets[1].url || saved.Kind != store.KindOpenAI {
		t.Fatalf("stored provider %+v", saved)
	}
}

func TestEditingAProviderOffersNoPresets(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	provider := world.addOpenAI(t, "Mine", "https://example.test/v1", "k")
	world.state.aiOpenProviderForm(provider.ID)
	canvas, _ := world.paint()
	if _, ok := hitByID(canvas, hitAISetFormPreset); ok {
		t.Fatal("the preset list is offered while editing")
	}
}

func TestAskAgentModeSurvivesAReload(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.setAIMode(agent.ModeAgent)
	if got := settingValue(t, world.db(t), "ai.mode"); got != "agent" {
		t.Fatalf("stored ai.mode = %q", got)
	}
	state.ai.mode = agent.ModeAsk // as a fresh window starts
	state.reloadAI()
	if state.ai.mode != agent.ModeAgent {
		t.Fatalf("reloaded mode = %v, want Agent", state.ai.mode)
	}
	state.setAIMode(agent.ModeAsk)
	if got := settingValue(t, world.db(t), "ai.mode"); got != "ask" {
		t.Fatalf("stored ai.mode = %q, want ask", got)
	}
}
