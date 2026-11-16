package workbench

import (
	"encoding/json"
	"motty/internal/ai/store"
	"strings"
	"testing"
)

func TestCLIProviderSyncKeepsDestinationExecutableLocal(t *testing.T) {
	w := newSettingsWorld(t, newLister("default"))
	s := w.state
	db, _ := s.aiStore()
	if err := db.SaveProvider(store.Provider{ID: "cli", Name: "Local", Kind: "codex-cli", CLIPath: "/destination/codex", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.reloadAI()
	records := s.savedAICLIProviders()
	data, _ := json.Marshal(records)
	if strings.Contains(string(data), "/destination/") {
		t.Fatal("local path exported")
	}
	s.importAIProviderCollections(nil, &[]savedAIProvider{{ID: "cli", Name: "Renamed", Kind: "codex-cli", Enabled: true, Models: []savedAIModel{{Name: "default", Enabled: true}}}})
	p, ok := s.aiProviderByID("cli")
	if !ok || p.Name != "Renamed" || p.CLIPath != "/destination/codex" {
		t.Fatal(p, ok)
	}
	s.importAIProviderCollections(nil, &[]savedAIProvider{{ID: "new", Name: "New", Kind: "opencode-cli", Enabled: true}})
	p, ok = s.aiProviderByID("new")
	if !ok || p.CLIPath != "" {
		t.Fatal(p, ok)
	}
}
