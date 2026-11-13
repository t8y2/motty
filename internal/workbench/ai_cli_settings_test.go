package workbench

import (
	"github.com/vivaui/viva/text"
	"motty/internal/ai/store"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIFormReflectsAutomaticPathWithoutPinningIt(t *testing.T) {
	for _, tc := range []struct{ kind, program string }{
		{"codex-cli", "codex"}, {"claude-code-cli", "claude"}, {"opencode-cli", "opencode"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			bin := t.TempDir()
			program := tc.program
			if runtime.GOOS == "windows" {
				program += ".exe"
			}
			path := filepath.Join(bin, program)
			if err := os.WriteFile(path, nil, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			w := newSettingsWorld(t, newLister("default"))
			s := w.state
			p := store.Provider{ID: "automatic", Name: "Local agent", Kind: tc.kind, Enabled: true}
			if err := w.db(t).SaveProvider(p); err != nil {
				t.Fatal(err)
			}
			s.reloadAI()
			s.aiOpenProviderForm(p.ID)
			if got := s.ai.cfg.cliPath.String(); got != path {
				t.Fatalf("automatic lookup did not populate the path input: got %q, want %q", got, path)
			}
			_, texts := w.paint()
			if !strings.Contains(strings.Join(texts, "|"), path) {
				t.Fatal("resolved path was not rendered")
			}
			if stored, _ := s.aiProviderByID(p.ID); stored.CLIPath != "" {
				t.Fatal("opening the form changed the saved automatic path")
			}
			s.aiSubmitProvider()
			s.cancelAIFetch()
			if stored, _ := s.aiProviderByID(p.ID); stored.CLIPath != "" {
				t.Fatal("saving an untouched discovered path pinned the executable")
			}
			s.aiOpenProviderForm(p.ID)
			s.ai.cfg.cliPath.SetSelection(text.Range{Start: 0, End: s.ai.cfg.cliPath.RuneCount()})
			s.ai.cfg.cliPath.Insert("/manual path/agent")
			s.aiSubmitProvider()
			s.cancelAIFetch()
			if stored, _ := s.aiProviderByID(p.ID); stored.CLIPath != "/manual path/agent" {
				t.Fatal("manual override was not saved")
			}
		})
	}
}

func TestCLIFormHidesHTTPInputsAndRoundTripsPath(t *testing.T) {
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli", "pi-cli"} {
		t.Run(kind, func(t *testing.T) {
			w := newSettingsWorld(t, newLister("default"))
			s := w.state
			s.Click(hitAISetProviderAdd)
			preset := -1
			for i, p := range aiPresets {
				if p.kind == kind {
					preset = i
					break
				}
			}
			if preset < 0 {
				t.Fatal("CLI preset missing", kind)
			}
			s.aiChoosePreset(preset)
			if s.EditorFor(hitAISetFormURL) != nil || s.EditorFor(hitAISetFormKey) != nil {
				t.Fatal("HTTP editor exposed")
			}
			w.typeInto(t, hitAISetFormCLIPath, "/local path/agent")
			_, texts := w.paint()
			joined := strings.Join(texts, "|")
			if strings.Contains(joined, s.messages.AISettings.FormKey) || strings.Contains(joined, s.messages.AISettings.FormBaseURL) {
				t.Fatal("HTTP fields painted", joined)
			}
			s.Click(hitAISetFormSubmit)
			s.cancelAIFetch()
			if s.ai.cfg.dialog != aiDialogNone || len(s.ai.providers) != 1 {
				t.Fatal("save failed", s.ai.cfg.formErr)
			}
			p := s.ai.providers[0]
			if p.Kind != kind || p.CLIPath != "/local path/agent" || p.HasKey || p.BaseURL != "" {
				t.Fatal(p)
			}
			p.Name = "Renamed provider"
			if err := w.db(t).SaveProvider(p); err != nil {
				t.Fatal(err)
			}
			s.reloadAI()
			s.aiOpenProviderForm(p.ID)
			if s.ai.cfg.cliPath.String() != "/local path/agent" {
				t.Fatal("edit lost local path")
			}
			_, editTexts := w.paint()
			identityShown := false
			for _, value := range editTexts {
				if strings.HasPrefix(value, s.messages.AISettings.FormEditTitle+" (") && strings.Contains(value, aiPresets[preset].label) {
					identityShown = true
				}
			}
			if !identityShown {
				t.Fatal("editing a renamed CLI provider hides its type")
			}
		})
	}
}

func TestUnknownProviderDoesNotFallBackToOpenAI(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	w.state.options.AIProvider = nil
	p := w.state.aiProvider(store.Provider{Kind: "not-a-provider"})
	if _, err := p.Models(t.Context()); err == nil || !strings.Contains(err.Error(), "unsupported_provider") {
		t.Fatal(err)
	}
}

func TestCLIModelRefreshDoesNotClaimRemoteDiscovery(t *testing.T) {
	w := newSettingsWorld(t, newLister("default"))
	s := w.state
	db, _ := s.aiStore()
	db.SaveProvider(store.Provider{ID: "cli", Name: "CLI", Kind: "codex-cli", Enabled: true})
	s.reloadAI()
	s.ai.cfg.selected = "cli"
	s.ai.cfg.fetched = true
	s.ai.cfg.fetchCount = 1
	text, failed := s.aiModelsStatus()
	if failed || text != s.messages.AISettings.CLIModelsNote {
		t.Fatal("static default mislabeled as discovery", text)
	}
}

func TestSwitchingCLIPresetsDropsThePreviousExecutable(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	s.aiOpenProviderForm("")
	claude, codex := -1, -1
	for i, preset := range aiPresets {
		switch preset.kind {
		case store.KindClaudeCLI:
			claude = i
		case store.KindCodexCLI:
			codex = i
		}
	}
	if claude < 0 || codex < 0 {
		t.Fatal("CLI presets missing")
	}
	s.aiChoosePreset(claude)
	s.ai.cfg.cliPath.Insert("/opt/claude")
	s.aiChoosePreset(codex)
	if got := s.ai.cfg.cliPath.String(); strings.Contains(got, "/opt/claude") {
		t.Fatalf("switching from Claude Code CLI to Codex CLI kept path %q", got)
	}
}

func TestDeletingKeylessCLIProviderDoesNotUnlockTheVault(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	s := w.state
	path, _ := os.Executable()
	db, _ := s.aiStore()
	if err := db.SaveProvider(store.Provider{ID: "cli", Name: "CLI", Kind: "codex-cli", CLIPath: path, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	s.reloadAI()
	if err := s.secrets.vault.LegacyForTests("master password"); err != nil {
		t.Fatal(err)
	}
	s.secrets.declined = true
	s.secrets.vault.DropKeyForTests()
	s.aiDeleteProvider("cli")
	if s.modal != modalNone {
		t.Fatalf("deleting a keyless CLI provider opened modal %d", s.modal)
	}
	if _, ok := s.aiProviderByID("cli"); ok {
		t.Fatal("keyless CLI provider was not deleted")
	}
}
