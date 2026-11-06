package workbench

import (
	"encoding/json"
	"motty/internal/ai/store"
	"motty/internal/backup"
	"motty/internal/configstore"
	"os"
	"testing"
)

func addCLIRecordFixture(t *testing.T, s *State) {
	t.Helper()
	db, err := s.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []store.Provider{{ID: "http", Name: "HTTP", Kind: "openai", BaseURL: "https://example.test", Enabled: true}, {ID: "cli", Name: "CLI", Kind: "codex-cli", CLIPath: "/destination/codex", Enabled: true}} {
		if err := db.SaveProvider(p); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveModels(p.ID, []store.Model{{ProviderID: p.ID, Name: "default", Enabled: true}}); err != nil {
			t.Fatal(err)
		}
	}
	s.reloadAI()
	s.save()
}

func TestCLIRecordsHaveLegacySafeIndependentOwnership(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	addCLIRecordFixture(t, s)
	entries, _, _, err := splitState(s.snapshot(), newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("MOTTY_LEGACY_RECORD_FIXTURE"); path != "" {
		raw, _ := json.Marshal(entries)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var cliKind, httpKind string
	for _, e := range entries {
		if e.ID == "cli" {
			cliKind = e.Kind
		}
		if e.ID == "http" {
			httpKind = e.Kind
		}
		if e.ID == "cli" && string(e.Payload) != "" {
			var p recordPayload
			json.Unmarshal(e.Payload, &p)
			var fields map[string]any
			json.Unmarshal(p.Data, &fields)
			if _, ok := fields["cli_path"]; ok {
				t.Fatal("path exported")
			}
		}
	}
	if cliKind != "ai-cli-provider" || httpKind != "ai-provider" {
		t.Fatalf("legacy client can claim CLI record: CLI=%q HTTP=%q", cliKind, httpKind)
	}
}

func TestUnknownFutureProviderSubtypeIsOpaqueAndCannotBeDeleted(t *testing.T) {
	for _, kind := range []string{"ai-provider", "ai-cli-provider"} {
		t.Run(kind, func(t *testing.T) {
			raw, _ := json.Marshal(recordPayload{Data: json.RawMessage(`{"id":"future","name":"Future","kind":"future-provider"}`)})
			_, base, err := joinState([]configstore.Entry{{Kind: kind, ID: "future", Payload: raw}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, owned := base.payloads[configstore.Key{Kind: kind, ID: "future"}]; owned {
				t.Fatal("unsupported provider became deletion-owned")
			}
			entries, _, _, err := splitState(savedState{}, base, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, deletes := base.diff(entries)
			if len(deletes) > 0 {
				t.Fatal("future provider tombstoned", deletes)
			}
		})
	}
}

func TestCLIAndHTTPCollectionsAreIndependentlyAuthoritative(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	addCLIRecordFixture(t, s)
	http := []savedAIProvider{{ID: "http", Name: "Changed HTTP", Kind: "openai", BaseURL: "https://example.test", Enabled: true}}
	s.restoreShared(savedState{AIProviders: &http})
	if p, ok := s.aiProviderByID("cli"); !ok || p.CLIPath != "/destination/codex" {
		t.Fatal("absent CLI collection deleted local configuration", p)
	}
	cli := []savedAIProvider{{ID: "cli", Name: "Changed CLI", Kind: "codex-cli", Enabled: true, Models: []savedAIModel{{Name: "default", Enabled: true}}}}
	s.restoreShared(cliCollectionState(t, cli))
	if p, ok := s.aiProviderByID("http"); !ok || p.Name != "Changed HTTP" {
		t.Fatal("CLI-only import deleted HTTP", p)
	}
	if p, _ := s.aiProviderByID("cli"); p.CLIPath != "/destination/codex" {
		t.Fatal("local path overwritten", p)
	}
	empty := []savedAIProvider{}
	s.restoreShared(cliCollectionState(t, empty))
	if _, ok := s.aiProviderByID("cli"); ok {
		t.Fatal("explicit CLI deletion ignored")
	}
	if _, ok := s.aiProviderByID("http"); !ok {
		t.Fatal("CLI deletion affected HTTP")
	}
}

func TestCLIProviderFirstRestoreAndLostDatabaseAdoption(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	addCLIRecordFixture(t, s)
	s.Close()
	if err := os.Remove(w.options.AIStorePath); err != nil {
		t.Fatal(err)
	}
	restored := w.open(t)
	if _, err := restored.aiStore(); err != nil {
		t.Fatal(err)
	}
	if p, ok := restored.aiProviderByID("cli"); !ok || p.Kind != "codex-cli" || p.CLIPath != "" {
		t.Fatal("CLI record not adopted", p, ok)
	}
	if len(restored.ai.models) != 2 {
		t.Fatal("adoption lost model choices", restored.ai.models)
	}
}

func TestOpeningSettingsKeepsCLIRecordBesideHTTPProvider(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	addCLIRecordFixture(t, s)
	db, err := s.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProvider("cli"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// w.open clicks Settings, which opens the assistant database and saves the
	// records. The HTTP row must not stop the missing CLI record from being
	// adopted, and the save must not tombstone it.
	restored := w.open(t)
	p, ok := restored.aiProviderByID("cli")
	if !ok || p.Kind != "codex-cli" {
		t.Fatalf("CLI provider was not adopted beside HTTP: %+v, %t", p, ok)
	}
	if !restored.recordedProviderIDs(kindAICLIProvider)["cli"] {
		t.Fatal("opening Settings deleted the CLI record")
	}
	if _, ok := restored.aiProviderByID("http"); !ok {
		t.Fatal("HTTP provider disappeared")
	}
}

func TestLegacyBackupWithoutCLICollectionKeepsLocalCLI(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	addCLIRecordFixture(t, s)
	if _, err := s.applyBackup(backup.Document{Format: configFormat}); err != nil {
		t.Fatal(err)
	}
	if p, ok := s.aiProviderByID("cli"); !ok || p.CLIPath != "/destination/codex" {
		t.Fatal("legacy backup deleted CLI provider", p)
	}
	if _, ok := s.aiProviderByID("http"); ok {
		t.Fatal("legacy HTTP replacement semantics changed")
	}
}

func TestDeletingLastSyncedCLIProviderKeepsHTTPProvider(t *testing.T) {
	server, _ := newFakeDAV(t)
	first, _, _ := backupTestState(t)
	second, _, _ := backupTestState(t)
	configureSync(t, first, server)
	configureSync(t, second, server)
	done := first.messages.Tools.SyncDone
	addCLIRecordFixture(t, first)
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if p, ok := second.aiProviderByID("cli"); !ok || p.CLIPath != "" {
		t.Fatal("first sync failed or exported machine path", p, ok)
	}
	if len(second.ai.models) != 2 {
		t.Fatal("sync lost CLI/HTTP models", second.ai.models)
	}
	first.aiDeleteProvider("cli")
	syncAndWait(t, first, hitSyncNow, done)
	syncAndWait(t, second, hitSyncNow, done)
	if _, ok := second.aiProviderByID("cli"); ok {
		t.Fatal("last CLI provider deletion did not sync")
	}
	if _, ok := second.aiProviderByID("http"); !ok {
		t.Fatal("CLI deletion also deleted HTTP provider")
	}
}

func cliCollectionState(t *testing.T, list []savedAIProvider) savedState {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"ai_cli_providers": list})
	if err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}
