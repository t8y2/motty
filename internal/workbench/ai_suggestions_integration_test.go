package workbench

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"motty/internal/ai/agent"
	"motty/internal/ai/attach"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
)

func TestSuggestionRefreshRotatesWithoutSending(t *testing.T) {
	state, _ := panelState(t, nil)
	withModel(t, state)
	state.Click(hitAIToggle)
	canvas, _ := paintAt(state, panelWide)
	first := panelHit(t, canvas, hitAIExamples.At(0)).Label
	refresh := tree.ID(aiPanelHits, "ai-examples-refresh")
	panelHit(t, canvas, refresh)
	state.Click(refresh)
	canvas, _ = paintAt(state, panelWide)
	if next := panelHit(t, canvas, hitAIExamples.At(0)).Label; next == first {
		t.Fatalf("refresh left the first example unchanged: %q", next)
	}
	if state.ai.input.String() != "" || state.ai.run != nil {
		t.Fatal("refresh populated or sent the composer")
	}
}

var boundSuggestionNonce = regexp.MustCompile(`GOPAI_ ([0-9a-f]{16}) `)

func prepareBoundSuggestion(t *testing.T, model *scriptedModel, command string) (*State, *fakeShell) {
	t.Helper()
	state, shell := assistantTestSession(t, model)
	shell.onWrite = func(data []byte) {
		match := boundSuggestionNonce.FindSubmatch(data)
		if len(match) == 2 {
			shell.say("\r\nfixture-output password=synthetic-private\r\n\r\nGOPAI_" + string(match[1]) + "0\r\n")
		}
	}
	state.setAIPanelOpen(true)
	state.setAIMode(agent.ModeAgent)
	entry := state.aiSuggestionPool(aiSuggestionConnected)[0]
	entry.Command = command
	if err := state.aiSaveSuggestion(entry); err != nil {
		t.Fatal(err)
	}
	canvas, _ := paintAt(state, panelWide)
	panelHit(t, canvas, hitAIExamples.At(0))
	state.Click(hitAIExamples.At(0))
	if state.aiBoundSuggestion() == nil {
		t.Fatal("click did not bind the configured command")
	}
	return state, shell
}

func TestBoundSuggestionSharesConversationScopeWithAgentTools(t *testing.T) {
	const bound, next = "touch synthetic-bound", "touch synthetic-next"
	model := &scriptedModel{turn: func(index int, request llm.Request) []llm.Event {
		if index == 0 {
			return toolTurn("terminal_exec", `{"command":"touch synthetic-next"}`)
		}
		return textTurn("analyzed")
	}}
	state, shell := prepareBoundSuggestion(t, model, bound)
	defer state.aiStop()
	if strings.Contains(shell.typed(), bound) || len(model.requests) != 0 {
		t.Fatal("selecting a card sent a request or executed the command")
	}
	state.ai.settings.autoUpTo = risk.High
	state.aiSetAutoUpTo(risk.Low)
	state.ai.attachments = []aiAttachment{{file: attach.File{
		Name: "fixture.png", Kind: attach.KindImage, Image: llm.Image{MediaType: llm.ImagePNG, Data: redSquarePNG(t)},
	}}}
	canvas, _ := paintAt(state, panelWide)
	if got := panelHit(t, canvas, hitAIBoundCommand).Label; !strings.Contains(got, bound) {
		t.Fatalf("command is not disclosed: %q", got)
	}
	state.aiPanelSend()
	pumpUntil(t, state, "bound approval", func() bool { return len(state.ai.approvals) != 0 })
	if approval := state.ai.approvals[0].request; approval.Summary != bound || approval.Session != state.ai.run.target.ID {
		t.Fatalf("wrong approval target or command: %+v", approval)
	}
	if strings.Contains(shell.typed(), bound) || len(model.requests) != 0 {
		t.Fatal("bound command ran or reached model before approval")
	}
	state.aiSetAutoUpTo(risk.Medium)
	if len(state.ai.approvals) != 1 {
		t.Fatal("scope change automatically cleared pending approval")
	}
	state.aiAnswerApproval(true)
	pumpUntil(t, state, "bound and follow-up runs", func() bool { return state.ai.run == nil })
	if typed := shell.typed(); strings.Count(typed, bound) != 1 || strings.Count(typed, next) != 1 {
		t.Fatalf("expected one execution of each synthetic command, got %q", typed)
	}
	if len(model.requests) != 2 || len(model.requests[0].Messages) != 3 {
		t.Fatalf("model started without the bound exchange: %v", model.requests)
	}
	messages := model.requests[0].Messages
	if len(messages[0].Images) != 1 {
		t.Fatal("pre-model command dropped the user attachment")
	}
	if messages[2].Role != llm.RoleTool || !strings.Contains(messages[2].Text, "fixture-output") || strings.Contains(messages[2].Text, "synthetic-private") {
		t.Fatalf("unredacted or missing model tool output: %+v", messages[2])
	}
}

func TestBoundSuggestionDeniedOrCanceledNeverRuns(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancel), func(t *testing.T) {
			const command = "touch synthetic-never"
			model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("analyzed") }}
			state, shell := prepareBoundSuggestion(t, model, command)
			state.aiPanelSend()
			pumpUntil(t, state, "bound approval", func() bool { return len(state.ai.approvals) != 0 })
			if cancel {
				state.aiStop()
			} else {
				state.aiAnswerApproval(false)
			}
			pumpUntil(t, state, "bound approval exit", func() bool { return state.ai.run == nil })
			if strings.Contains(shell.typed(), command) || len(model.requests) != 0 || state.ai.suggestions.armed != nil {
				t.Fatal("denied or canceled command ran, reached model, or stayed armed")
			}
		})
	}
}

func TestBoundSuggestionNeedsAnUneditedExplicitSend(t *testing.T) {
	for _, mode := range []string{"edited", "internal send", "ask"} {
		t.Run(mode, func(t *testing.T) {
			const command = "echo synthetic-never"
			model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("analyzed") }}
			state, shell := prepareBoundSuggestion(t, model, command)
			before := state.ai.input.String()
			switch mode {
			case "edited":
				state.aiPanelText(app.TextEvent{Kind: app.TextInsert, Text: "?"})
				state.aiPanelText(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextDeleteBackward})
			case "ask":
				state.setAIMode(agent.ModeAsk)
			}
			if state.ai.input.String() != before {
				t.Fatal("fixture changed the final text")
			}
			if mode == "internal send" {
				state.aiSend()
			} else {
				state.aiPanelSend()
			}
			pumpUntil(t, state, "plain answer", func() bool { return state.ai.run == nil })
			if strings.Contains(shell.typed(), command) || len(model.requests) != 1 || len(model.requests[0].Messages) != 1 {
				t.Fatal("plain send inherited a bound command")
			}
		})
	}
}

func TestAISuggestionsRequireConfirmationAndFreezeThePreview(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event {
		return textTurn(`["Question one?","Question two?","Question three?"]`)
	}}
	state, shell := assistantTestSession(t, model)
	shell.say("\r\nunique-before-preview password=synthetic-private \x1b[8mconcealed-value\x1b[28m\r\n")
	state.Wake()
	state.setAIPanelOpen(true)
	canvas, _ := paintAt(state, panelWide)
	panelHit(t, canvas, hitAIPreviewSuggestions)
	if state.ai.suggestions.request != nil || state.ai.suggestions.preview != nil || len(model.requests) != 0 {
		t.Fatal("opening the assistant initiated a suggestion request")
	}
	state.Click(hitAIPreviewSuggestions)
	preview := state.ai.suggestions.preview
	if preview == nil || !strings.Contains(preview.payload, "unique-before-preview") || strings.Contains(preview.payload, "synthetic-private") || strings.Contains(preview.payload, "concealed-value") {
		t.Fatalf("preview was not redacted: %+v", preview)
	}
	canvas, texts := paintAt(state, panelWide)
	if !hasText(texts, "unique-before-preview") || state.ai.suggestions.request != nil || len(model.requests) != 0 {
		t.Fatal("preview was not visible before sending")
	}
	panelHit(t, canvas, hitAIGenerateSuggestions)
	panelHit(t, canvas, hitAICancelSuggestionPreview)
	shell.say("\r\nnew-terminal-content\r\n")
	state.Wake()
	clickAIPanelPage(t, state, hitAIGenerateSuggestions)
	if state.ai.suggestions.request == nil {
		t.Fatal("clicking the confirmation button did not start generation")
	}
	state.aiConfirmSuggestionPreview(preview)
	pumpUntil(t, state, "generated cards", func() bool { return len(state.ai.suggestions.generated) == 3 })
	if len(model.requests) != 1 || len(model.requests[0].Tools) != 0 || len(model.requests[0].Messages) != 1 || model.requests[0].Messages[0].Text != preview.payload {
		t.Fatalf("request did not contain precisely the frozen preview and no tools/history: %v", model.requests)
	}
	if len(state.ai.history) != 0 || len(state.ai.items) != 0 {
		t.Fatal("suggestion context was stored in conversation history")
	}
	paintAt(state, panelWide)
	state.Click(hitAIExamples.At(0))
	if state.ai.input.String() != "Question one?" || state.ai.suggestions.armed != nil {
		t.Fatal("generated prompt was not click-only and command-free")
	}
}

func TestAISuggestionPreviewRejectsAChangedTarget(t *testing.T) {
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn(`[]`) }}
	state := targetTestWorld(t, model)
	state.setAIPanelOpen(true)
	state.aiSetSelected([]uint32{state.sessions[0].ID})
	state.aiOpenSuggestionPreview()
	preview := state.ai.suggestions.preview
	if preview == nil {
		t.Fatal("first target did not open a preview")
	}
	state.aiSetSelected([]uint32{state.sessions[1].ID})
	state.aiSetSelected([]uint32{state.sessions[0].ID})
	state.aiConfirmSuggestionPreview(preview)
	if state.ai.suggestions.preview != nil || state.ai.suggestions.request != nil || len(model.requests) != 0 {
		t.Fatal("preview remained sendable after changing away from its target")
	}
}

func TestAISuggestionsUseSelectedProvidersProxyAndVaultSecrets(t *testing.T) {
	requests := make(chan struct {
		host, auth, proxyAuth, model, prompt string
		tools                                int
	}, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		var body struct {
			Model    string
			Messages []struct{ Role, Content string }
			Tools    []json.RawMessage
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.Messages) != 2 {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		requests <- struct {
			host, auth, proxyAuth, model, prompt string
			tools                                int
		}{req.URL.Host, req.Header.Get("Authorization"), req.Header.Get("Proxy-Authorization"), body.Model, body.Messages[1].Content, len(body.Tools)}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"[\\\"One?\\\",\\\"Two?\\\",\\\"Three?\\\"]\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer proxy.Close()
	state, _ := panelState(t, nil)
	withModel(t, state)
	const providerID = "selected-proxy"
	if err := state.secrets.vault.Set(aiKeySecretName(providerID), "synthetic-key"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(aiProxyPasswordSecretName(providerID), "synthetic-password"); err != nil {
		t.Fatal(err)
	}
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveProvider(store.Provider{ID: providerID, Name: "selected", Kind: store.KindOpenAI, Enabled: true,
		BaseURL: "http://fixture.invalid/v1", HasKey: true, ProxyURL: strings.Replace(proxy.URL, "http://", "http://fixture-user@", 1), HasProxyPassword: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveModels(providerID, []store.Model{{Name: "fixture-model", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	state.reloadAI()
	state.ai.modelProvider, state.ai.modelName, state.ai.modelPinned = providerID, "fixture-model", true
	state.setAIPanelOpen(true)
	state.aiOpenSuggestionPreview()
	preview := state.ai.suggestions.preview
	if preview == nil {
		t.Fatal("selected HTTP provider failed to preview")
	}
	state.aiConfirmSuggestionPreview(preview)
	pumpUntil(t, state, "local proxy response", func() bool { return state.ai.suggestions.request == nil })
	select {
	case got := <-requests:
		wantProxyAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("fixture-user:synthetic-password"))
		if got.host != "fixture.invalid" || got.auth != "Bearer synthetic-key" || got.proxyAuth != wantProxyAuth || got.model != "fixture-model" || got.prompt != preview.payload || got.tools != 0 {
			t.Fatalf("wrong request route or preview: %+v", got)
		}
	default:
		t.Fatal("request did not pass through the provider proxy")
	}
	if len(state.ai.suggestions.generated) != 3 {
		t.Fatal("proxy result did not populate suggestions")
	}
}
