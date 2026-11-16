package workbench

import (
	"context"
	"motty/internal/ai/agent"
	"motty/internal/ai/cli"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"motty/internal/i18n"
	"testing"
)

func TestSignedOutCLIHintAllowsThirdPartyConfiguration(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		name := "en"
		if language == i18n.Chinese {
			name = "zh"
		}
		t.Run(name, func(t *testing.T) {
			w := newSettingsWorld(t, newLister("default"))
			s := w.state
			s.setLanguage(language)
			db, err := s.aiStore()
			if err != nil {
				t.Fatal(err)
			}
			if err := db.SaveProvider(store.Provider{ID: "cli", Name: "Configured CLI", Kind: "codex-cli", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			s.reloadAI()
			s.ai.cfg.selected = "cli"
			s.aiSetAuth("cli", cli.AuthSignedOut)
			_, texts := w.paint()
			hint := "third-party"
			if language == i18n.Chinese {
				hint = "第三方"
			}
			if !hasText(texts, hint) {
				t.Fatal("signed-out status wrongly implies official login is required", texts)
			}
		})
	}
}

type optionalAuthSession struct {
	quietModel
	requests chan llm.Request
}

func (p *optionalAuthSession) RunSession(_ context.Context, req llm.Request, _ func(context.Context, llm.ToolCall) gate.Result, emit func(llm.Event)) error {
	p.requests <- req
	emit(llm.Event{Kind: llm.EventText, Text: "Third-party route answered."})
	emit(llm.Event{Kind: llm.EventDone})
	return nil
}

func TestSignedOutCLIStillSendsWithoutOfficialLogin(t *testing.T) {
	for _, kind := range []string{"claude-code-cli", "codex-cli", "opencode-cli"} {
		t.Run(kind, func(t *testing.T) {
			w := newSettingsWorld(t, newLister("default"))
			s := w.state
			db, err := s.aiStore()
			if err != nil {
				t.Fatal(err)
			}
			p := store.Provider{ID: "cli", Name: "Third-party route", Kind: kind, CLIPath: "/configured/local-cli", Enabled: true}
			if err := db.SaveProvider(p); err != nil {
				t.Fatal(err)
			}
			if err := db.SetDefaultModel(p.ID, "default"); err != nil {
				t.Fatal(err)
			}
			s.reloadAI()
			s.aiSetAuth(p.ID, cli.AuthSignedOut)
			session := &optionalAuthSession{requests: make(chan llm.Request, 1)}
			s.options.AIProvider = func(actual store.Provider) llm.Provider {
				if actual.Kind != kind || actual.CLIPath != p.CLIPath || actual.HasKey || actual.APIKey != "" {
					t.Error("CLI route/path changed or Motty vault credentials injected", actual)
				}
				return session
			}
			ask(s, "Use my existing provider", agent.ModeAsk)
			pumpUntil(t, s, "third-party answer", func() bool { return s.ai.run == nil })
			select {
			case req := <-session.requests:
				if req.Model != "default" {
					t.Fatal("CLI default model replaced", req.Model)
				}
			default:
				t.Fatal("signed-out hint blocked a send")
			}
			if s.ai.login != nil || s.ai.needLogin != "" || len(s.ai.history) != 2 || s.ai.history[1].Text != "Third-party route answered." {
				t.Fatal("login forced or successful answer lost", s.ai.history)
			}
		})
	}
}
