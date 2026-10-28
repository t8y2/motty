package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/gate"
	"motty/internal/ai/mcp"
	"motty/internal/ai/store"
	"motty/internal/aiproviderimport"
	"motty/internal/vault"
)

// The assistant's settings section is a part of the Settings page. Its state
// lives in aiState.cfg; everything it persists goes to the AI database, one
// write per choice, and is read back by reloadAI so the page and the engine
// always see the stored values.

// Hit identities of the assistant's settings section are names in a place of
// their own; its lists are scopes whose rows are numbered items.
//
// aiSettingsHits is the place of the controls this file paints.
var aiSettingsHits = tree.Feature("ai-settings")

var (
	hitAISetProviderAdd       = tree.ID(aiSettingsHits, "ai-set-provider-add")
	hitAISetProviderImport    = tree.ID(aiSettingsHits, "ai-set-provider-import")
	hitAISetImportSource      = tree.Sub(aiSettingsHits, tree.Name("ai-set-import-source")) // preview, per source
	hitAISetImportChoose      = tree.ID(aiSettingsHits, "ai-set-import-choose")
	hitAISetImportBack        = tree.ID(aiSettingsHits, "ai-set-import-back")
	hitAISetImportApply       = tree.ID(aiSettingsHits, "ai-set-import-apply")
	hitAISetImportClose       = tree.ID(aiSettingsHits, "ai-set-import-close")
	hitAISetImportList        = tree.ID(aiSettingsHits, "ai-set-import-list")            // scroll state and group hit
	hitAISetImportRow         = tree.Sub(aiSettingsHits, tree.Name("ai-set-import-row")) // select, per candidate
	hitAISetFormDismiss       = tree.ID(aiSettingsHits, "ai-set-form-dismiss")
	hitAISetFormClose         = tree.ID(aiSettingsHits, "ai-set-form-close")
	hitAISetFormCancel        = tree.ID(aiSettingsHits, "ai-set-form-cancel")
	hitAISetFormSubmit        = tree.ID(aiSettingsHits, "ai-set-form-submit")
	hitAISetFormKindOpenAI    = tree.ID(aiSettingsHits, "ai-set-form-kind-open-ai")
	hitAISetFormKindClaude    = tree.ID(aiSettingsHits, "ai-set-form-kind-claude")
	hitAISetFormAuthAPIKey    = tree.ID(aiSettingsHits, "ai-set-form-auth-api-key")
	hitAISetFormAuthBearer    = tree.ID(aiSettingsHits, "ai-set-form-auth-bearer")
	hitAISetFormPreset        = tree.ID(aiSettingsHits, "ai-set-form-preset")
	hitAISetFormPresetDismiss = tree.ID(aiSettingsHits, "ai-set-form-preset-dismiss")
	hitAISetFormPresetItems   = tree.Sub(aiSettingsHits, tree.Name("ai-set-form-preset-item"))
	hitAISetFormName          = tree.ID(aiSettingsHits, "ai-set-form-name")
	hitAISetFormURL           = tree.ID(aiSettingsHits, "ai-set-form-url")
	hitAISetFormKey           = tree.ID(aiSettingsHits, "ai-set-form-key")
	hitAISetFormCLIPath       = tree.ID(aiSettingsHits, "ai-set-form-cli-path")
	hitAISetConfirmCancel     = tree.ID(aiSettingsHits, "ai-set-confirm-cancel")
	hitAISetConfirmAction     = tree.ID(aiSettingsHits, "ai-set-confirm-action")
	hitAISetFetchModels       = tree.ID(aiSettingsHits, "ai-set-fetch-models")
	hitAISetModelInput        = tree.ID(aiSettingsHits, "ai-set-model-input")
	hitAISetModelAdd          = tree.ID(aiSettingsHits, "ai-set-model-add")
	hitAISetModelPrev         = tree.ID(aiSettingsHits, "ai-set-model-prev")
	hitAISetModelNext         = tree.ID(aiSettingsHits, "ai-set-model-next")
	hitAISetRiskLow           = tree.ID(aiSettingsHits, "ai-set-risk-low")
	hitAISetRiskMedium        = tree.ID(aiSettingsHits, "ai-set-risk-medium")
	hitAISetRiskHigh          = tree.ID(aiSettingsHits, "ai-set-risk-high")
	hitAISetSteps6            = tree.ID(aiSettingsHits, "ai-set-steps6")
	hitAISetSteps12           = tree.ID(aiSettingsHits, "ai-set-steps12")
	hitAISetSteps24           = tree.ID(aiSettingsHits, "ai-set-steps24")
	hitAISetContext0          = tree.ID(aiSettingsHits, "ai-set-context0")
	hitAISetContext30         = tree.ID(aiSettingsHits, "ai-set-context30")
	hitAISetContext60         = tree.ID(aiSettingsHits, "ai-set-context60")
	hitAISetContext120        = tree.ID(aiSettingsHits, "ai-set-context120")
	hitAISetRedact            = tree.ID(aiSettingsHits, "ai-set-redact")
	hitAISetHistory           = tree.ID(aiSettingsHits, "ai-set-history")
	hitAISetClearHistory      = tree.ID(aiSettingsHits, "ai-set-clear-history")
	hitAISetMCPEnabled        = tree.ID(aiSettingsHits, "ai-set-mcp-enabled")
	hitAISetMCPObserve        = tree.ID(aiSettingsHits, "ai-set-mcp-observe")
	hitAISetMCPAsk            = tree.ID(aiSettingsHits, "ai-set-mcp-ask")
	hitAISetMCPAllowAll       = tree.ID(aiSettingsHits, "ai-set-mcp-allow-all")
	hitAISetMCPCopy           = tree.ID(aiSettingsHits, "ai-set-mcp-copy")
	hitAISetMCPCopyURL        = tree.ID(aiSettingsHits, "ai-set-mcp-copy-url")
	hitAISetMCPCopyToken      = tree.ID(aiSettingsHits, "ai-set-mcp-copy-token")
	hitAISetMCPClients        = tree.Sub(aiSettingsHits, tree.Name("ai-set-mcp-client"))
	hitAISetMCPClaude         = aiMCPClientID(aiMCPClaude)
	hitAISetMCPCursor         = aiMCPClientID(aiMCPCursor)
	hitAISetMCPCodex          = aiMCPClientID(aiMCPCodex)
	hitAISetMCPReset          = tree.ID(aiSettingsHits, "ai-set-mcp-reset")
	hitAISetMCPSelect         = tree.ID(aiSettingsHits, "ai-set-mcp-select")
	hitAISetMCPSelectDismiss  = tree.ID(aiSettingsHits, "ai-set-mcp-select-dismiss")
)

// Rows of the provider and model lists have a block of identities each.
const (
	aiMaxProviderRows = 100
	aiModelPageSize   = 8
)

var (
	hitAISetProviderRow    = tree.Sub(aiSettingsHits, tree.Name("ai-set-provider-row"))    // select, per provider
	hitAISetProviderSwitch = tree.Sub(aiSettingsHits, tree.Name("ai-set-provider-switch")) // enable, per provider
	hitAISetProviderEdit   = tree.Sub(aiSettingsHits, tree.Name("ai-set-provider-edit"))
	hitAISetProviderDelete = tree.Sub(aiSettingsHits, tree.Name("ai-set-provider-delete"))
	hitAISetModelUse       = tree.Sub(aiSettingsHits, tree.Name("ai-set-model-use")) // per row of the shown page
	hitAISetModelSwitch    = tree.Sub(aiSettingsHits, tree.Name("ai-set-model-switch"))
)

// aiDialogKind is which of the section's dialogs is up.
type aiDialogKind uint8

const (
	aiDialogNone aiDialogKind = iota
	aiDialogProvider
	aiDialogDelete
	aiDialogClear
	aiDialogCLILogin
	aiDialogImport
	aiDialogMCPAllowAll
)

// aiCopyState is what the "copy client configuration" button last did.
type aiCopyState uint8

const (
	aiCopyIdle aiCopyState = iota
	aiCopyDone
	aiCopyFailed
)

// aiFetch is a model list being fetched from a provider.
type aiFetch struct {
	providerID string
	cancel     context.CancelFunc
}

// aiSettingsUI is the transient state of the settings section: the open
// dialog, the selected provider and the outcome of the last action. The saved
// choices themselves are in aiState.settings.
type aiSettingsUI struct {
	dialog aiDialogKind

	// The provider dialog. editID is empty when a provider is being added.
	editID string
	kind   string
	// preset is the entry of aiPresets the form was last filled from, open
	// whether its list is showing.
	preset     int
	presetOpen bool
	name       text.Editor
	baseURL    text.Editor
	apiKey     text.Editor
	cliPath    text.Editor
	authStyle  string // Anthropic only; empty means the API key default
	hadKey     bool
	formErr    string
	// deleteID is the provider the delete question is about.
	deleteID string

	selected   string // provider whose models are listed
	page       int    // page of that provider's models
	modelInput text.Editor

	fetch           *aiFetch
	fetched         bool // a fetch ended: with fetchCount models, or fetchErr
	fetchCount      int
	fetchErr        string
	modelErr        string
	notice          string // why the last change could not be stored
	clearedDone     bool
	copied          aiCopyState
	copyTarget      ui.ID
	mcpClient       aiMCPClient
	mcpSelectOpen   bool
	mcpSelectBounds ui.Rect
	mcpPreviewLines []string
	tokenReset      bool
	loadPosted      bool // the first load was queued from a paint

	// The provider import dialog. The candidates carry credentials until the
	// dialog closes or the import ends; only key presence is ever painted.
	importStep       aiImportStep
	importSources    []aiproviderimport.SourceInfo
	importCandidates []aiproviderimport.Candidate
	importSelected   map[int]bool
	importErr        string
	importResult     aiImportResult
}

func (state *State) aiSettingsVisible() bool {
	return state.activeSection == sectionSettings && state.modal == modalNone
}

// aiSettingsOpened loads the assistant's choices when the page is opened, so
// painting never touches the disk.
func (state *State) aiSettingsOpened() {
	_, _ = state.aiStore()
}

func (state *State) aiSettingsClick(id ui.ID) bool {
	if !state.aiSettingsVisible() {
		return false
	}
	cfg := &state.ai.cfg
	if cfg.dialog != aiDialogNone {
		state.aiDialogClick(id)
		return true // nothing behind a dialog is reachable
	}
	if !tree.Within(id.Scope(), aiSettingsHits) {
		return false
	}
	cfg.clearedDone, cfg.copied, cfg.tokenReset, cfg.notice = false, aiCopyIdle, false, ""
	switch {
	case id == hitAICLILogin:
		if provider, ok := state.aiSelectedProvider(); ok {
			state.aiStartCLILogin(provider.ID, false)
		}
	case id == hitAICLIDevice:
		if provider, ok := state.aiSelectedProvider(); ok {
			state.aiStartCLILogin(provider.ID, true)
		}
	case id == hitAICLIRecheck:
		if provider, ok := state.aiSelectedProvider(); ok {
			state.aiCheckCLIAuth(provider)
		}
	case id == hitAISetProviderAdd:
		state.aiOpenProviderForm("")
	case id == hitAISetProviderImport:
		state.aiOpenImport()
	case id.Scope() == hitAISetProviderRow:
		index, _ := id.In(hitAISetProviderRow)
		state.aiSelectProvider(index)
	case id.Scope() == hitAISetProviderSwitch:
		index, _ := id.In(hitAISetProviderSwitch)
		state.aiToggleProvider(index)
	case id.Scope() == hitAISetProviderEdit:
		if index, _ := id.In(hitAISetProviderEdit); index < len(state.ai.providers) {
			state.aiOpenProviderForm(state.ai.providers[index].ID)
		}
	case id.Scope() == hitAISetProviderDelete:
		if index, _ := id.In(hitAISetProviderDelete); index < len(state.ai.providers) {
			cfg.deleteID, cfg.dialog = state.ai.providers[index].ID, aiDialogDelete
		}
	case id == hitAISetFetchModels:
		state.aiFetchModels()
	case id == hitAISetModelAdd:
		state.aiAddModel()
	case id == hitAISetModelPrev:
		cfg.page = max(cfg.page-1, 0)
	case id == hitAISetModelNext:
		cfg.page++
		state.aiClampModelPage()
	case id.Scope() == hitAISetModelUse:
		index, _ := id.In(hitAISetModelUse)
		state.aiUseModel(index)
	case id.Scope() == hitAISetModelSwitch:
		index, _ := id.In(hitAISetModelSwitch)
		state.aiToggleModel(index)
	case id == hitAISetRiskLow:
		state.aiPut("ai.auto_up_to", "low")
	case id == hitAISetRiskMedium:
		state.aiPut("ai.auto_up_to", "medium")
	case id == hitAISetRiskHigh:
		state.aiPut("ai.auto_up_to", "high")
	case id == hitAISetSteps6:
		state.aiPut("ai.max_steps", "6")
	case id == hitAISetSteps12:
		state.aiPut("ai.max_steps", "12")
	case id == hitAISetSteps24:
		state.aiPut("ai.max_steps", "24")
	case id == hitAISetContext0:
		state.aiPut("ai.context_lines", "0")
	case id == hitAISetContext30:
		state.aiPut("ai.context_lines", "30")
	case id == hitAISetContext60:
		state.aiPut("ai.context_lines", "60")
	case id == hitAISetContext120:
		state.aiPut("ai.context_lines", "120")
	case id == hitAISetRedact:
		state.aiPut("ai.redact", onOff(!state.ai.settings.redact))
	case id == hitAISetHistory:
		state.aiPut("ai.history", onOff(!state.ai.settings.keepHistory))
	case id == hitAISetClearHistory:
		if state.ai.run == nil {
			cfg.dialog = aiDialogClear
		}
	case id == hitAISetMCPEnabled:
		state.aiPut("ai.mcp.enabled", onOff(!state.ai.settings.mcpEnabled))
	case id == hitAISetMCPObserve:
		state.aiPut("ai.mcp.mode", "observe")
	case id == hitAISetMCPAsk:
		state.aiPut("ai.mcp.mode", "ask")
	case id == hitAISetMCPAllowAll:
		if state.ai.settings.mcpMode != gate.ModeAllowAll {
			cfg.dialog = aiDialogMCPAllowAll
		}
	case state.aiAgentClick(id):
	case id == hitAISetMCPCopy:
		state.aiCopyClientConfig()
	case id == hitAISetMCPCopyURL:
		state.aiCopyMCP(id, state.aiMCPURL())
	case id == hitAISetMCPCopyToken:
		state.aiCopyMCP(id, state.ai.mcpToken)
	case id.Scope() == hitAISetMCPClients:
		if index, ok := id.In(hitAISetMCPClients); ok && index >= 0 && index < int(aiMCPClientCount) {
			cfg.mcpClient = aiMCPClient(index)
			cfg.mcpSelectOpen = false
		}
	case id == hitAISetMCPSelect:
		cfg.mcpSelectOpen = !cfg.mcpSelectOpen
	case id == hitAISetMCPSelectDismiss:
		cfg.mcpSelectOpen = false
	case id == hitAISetMCPReset:
		// An empty token makes the application create a fresh one when it next
		// reconfigures the server.
		if state.aiPut("ai.mcp.token", "") {
			cfg.tokenReset = true
			state.ai.mcpToken = "" // Wait for the hub to publish the replacement.
		}
	default:
		return false
	}
	return true
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func (state *State) aiDialogClick(id ui.ID) {
	cfg := &state.ai.cfg
	switch cfg.dialog {
	case aiDialogCLILogin:
		if id == hitAISetFormDismiss || id == hitAISetFormClose || id == hitAISetFormCancel {
			state.aiStopCLILogin()
		}
	case aiDialogProvider:
		if cfg.presetOpen {
			switch {
			case id == hitAISetFormPreset:
				cfg.presetOpen = false
			case id.Scope() == hitAISetFormPresetItems:
				state.aiChoosePreset(item(id))
			default:
				cfg.presetOpen = false
			}
			return
		}
		switch id {
		case hitAISetFormPreset:
			cfg.presetOpen = true
		case hitAISetFormDismiss, hitAISetFormClose, hitAISetFormCancel:
			state.aiCloseDialog()
		case hitAISetFormSubmit:
			state.aiSubmitProvider()
		case hitAISetFormKindOpenAI:
			if store.IsCLIKind(cfg.kind) {
				return
			}
			cfg.kind = store.KindOpenAI
		case hitAISetFormKindClaude:
			if store.IsCLIKind(cfg.kind) {
				return
			}
			cfg.kind = store.KindAnthropic
		case hitAISetFormAuthAPIKey:
			if cfg.kind == store.KindAnthropic {
				cfg.authStyle = store.AuthStyleAPIKey
			}
		case hitAISetFormAuthBearer:
			if cfg.kind == store.KindAnthropic {
				cfg.authStyle = store.AuthStyleBearer
			}
		}
	case aiDialogDelete:
		switch id {
		case hitAISetConfirmCancel:
			state.aiCloseDialog()
		case hitAISetConfirmAction:
			state.aiDeleteProvider(cfg.deleteID)
			state.aiCloseDialog()
		}
	case aiDialogClear:
		switch id {
		case hitAISetConfirmCancel:
			state.aiCloseDialog()
		case hitAISetConfirmAction:
			state.aiCloseDialog()
			state.aiClearHistory()
		}
	case aiDialogMCPAllowAll:
		switch id {
		case hitAISetConfirmCancel:
			state.aiCloseDialog()
		case hitAISetConfirmAction:
			if state.aiPut("ai.mcp.mode", "allow-all") {
				state.aiCloseDialog()
			}
		}
	case aiDialogImport:
		state.aiImportDialogClick(id)
	}
}

// aiImportDialogClick handles the import dialog's three steps.
func (state *State) aiImportDialogClick(id ui.ID) {
	cfg := &state.ai.cfg
	switch cfg.importStep {
	case aiImportSources:
		switch {
		case id == hitAISetFormDismiss || id == hitAISetFormClose || id == hitAISetImportClose:
			state.aiCloseDialog()
		case id.Scope() == hitAISetImportSource:
			if index, ok := id.In(hitAISetImportSource); ok {
				state.aiImportUseSource(index)
			}
		case id == hitAISetImportChoose:
			state.aiImportChooseFile()
		}
	case aiImportPreview:
		switch {
		case id == hitAISetFormDismiss || id == hitAISetFormClose:
			state.aiCloseDialog()
		case id == hitAISetImportBack:
			state.aiImportBack()
		case id == hitAISetImportApply:
			state.aiImportApply()
		case id.Scope() == hitAISetImportRow:
			if index, ok := id.In(hitAISetImportRow); ok {
				state.aiImportToggle(index)
			}
		}
	case aiImportDone:
		if id == hitAISetFormDismiss || id == hitAISetFormClose || id == hitAISetImportClose {
			state.aiCloseDialog()
		}
	}
}

// aiCloseDialog closes the open dialog and forgets what was typed in it, the
// API key first of all.
func (state *State) aiCloseDialog() {
	cfg := &state.ai.cfg
	cfg.dialog, cfg.editID, cfg.deleteID, cfg.formErr, cfg.hadKey, cfg.presetOpen = aiDialogNone, "", "", "", false, false
	cfg.name, cfg.baseURL, cfg.apiKey = text.Editor{}, text.Editor{}, text.Editor{}
	cfg.cliPath, cfg.authStyle = text.Editor{}, ""
	// The import candidates hold credentials in memory; closing the dialog is
	// also how they are forgotten.
	cfg.importStep, cfg.importErr = aiImportSources, ""
	cfg.importSources, cfg.importCandidates, cfg.importSelected = nil, nil, nil
	cfg.importResult = aiImportResult{}
}

func (state *State) aiSettingsPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if state.ai.login != nil && event.Target == hitAICLILoginTerminal {
		return ui.EventResult{Handled: true, Focus: hitAICLILoginTerminal}, true
	}
	return ui.EventResult{}, false
}

func (state *State) aiSettingsScroll(event ui.ScrollEvent) (ui.EventResult, bool) {
	if state.ai.login != nil && state.ai.cfg.dialog == aiDialogCLILogin {
		state.ai.login.term.screen.Scroll(int(event.DeltaY / max(state.cellHeight, 1)))
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

func (state *State) aiSettingsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.ai.login != nil && state.ai.cfg.dialog == aiDialogCLILogin {
		if event.Key == ui.KeyEscape {
			state.aiStopCLILogin()
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
		return state.aiCLILoginKey(event), true
	}
	if !state.aiSettingsVisible() {
		return ui.EventResult{}, false
	}
	cfg := &state.ai.cfg
	if cfg.dialog == aiDialogMCPAllowAll {
		return ui.EventResult{}, false // the alert's focus trap owns its keys
	}
	if event.Key == ui.KeyEnter && !event.Repeat {
		switch {
		case cfg.dialog == aiDialogProvider && (event.Target == hitAISetFormName || event.Target == hitAISetFormURL || event.Target == hitAISetFormKey || event.Target == hitAISetFormCLIPath):
			state.aiSubmitProvider()
			return ui.EventResult{Handled: true, Repaint: true}, true
		case cfg.dialog == aiDialogNone && event.Target == hitAISetModelInput:
			state.aiAddModel()
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	var options []components.SegmentOption
	if event.Target == hitAISetMCPSelect && cfg.dialog == aiDialogNone && !cfg.mcpSelectOpen && (event.Key == ui.KeyDown || event.Key == ui.KeyUp) {
		cfg.mcpSelectOpen = true
		return ui.EventResult{Focus: aiMCPClientID(cfg.mcpClient), FocusVisible: true, Handled: true, Repaint: true}, true
	}
	var kind [2]components.SegmentOption
	var auth [2]components.SegmentOption
	var risks [3]components.SegmentOption
	var steps [3]components.SegmentOption
	var lines [4]components.SegmentOption
	var modes [3]components.SegmentOption
	var agents [2]components.SegmentOption
	switch event.Target {
	case hitAISetFormKindOpenAI, hitAISetFormKindClaude:
		kind = state.aiKindOptions()
		options = kind[:]
	case hitAISetFormAuthAPIKey, hitAISetFormAuthBearer:
		auth = state.aiAuthOptions()
		options = auth[:]
	case hitAISetRiskLow, hitAISetRiskMedium, hitAISetRiskHigh:
		risks = state.aiRiskOptions()
		options = risks[:]
	case hitAISetSteps6, hitAISetSteps12, hitAISetSteps24:
		steps = state.aiStepsOptions()
		options = steps[:]
	case hitAISetContext0, hitAISetContext30, hitAISetContext60, hitAISetContext120:
		lines = state.aiContextOptions()
		options = lines[:]
	case hitAISetMCPObserve, hitAISetMCPAsk, hitAISetMCPAllowAll:
		modes = state.aiMCPModeOptions()
		options = modes[:]
	case hitAISetAgentObserve, hitAISetAgentRun:
		agents = state.aiAgentModeOptions()
		options = agents[:]
	default:
		return ui.EventResult{}, false
	}
	next, handled := components.SegmentedControlKeyDown(event.Target, options, components.DirectionLTR, event.Key)
	if !handled {
		return ui.EventResult{}, true
	}
	state.Click(next)
	if cfg.dialog == aiDialogMCPAllowAll {
		return ui.EventResult{Focus: hitAISetConfirmCancel, FocusVisible: true, Handled: true, Repaint: true}, true
	}
	return ui.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}, true
}

func (state *State) aiSettingsEditor(hit ui.ID) *text.Editor {
	if !state.aiSettingsVisible() || !tree.Within(hit.Scope(), aiSettingsHits) {
		return nil
	}
	cfg := &state.ai.cfg
	switch {
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormName:
		return &cfg.name
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormCLIPath && store.IsCLIKind(cfg.kind):
		return &cfg.cliPath
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormURL && !store.IsCLIKind(cfg.kind):
		return &cfg.baseURL
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormKey && !store.IsCLIKind(cfg.kind):
		return &cfg.apiKey
	case cfg.dialog == aiDialogNone && hit == hitAISetModelInput:
		cfg.modelInput.Restrict(modelNameRune, 200)
		return &cfg.modelInput
	}
	return nil
}

func (state *State) aiKindOptions() [2]components.SegmentOption {
	m := state.messages.AISettings
	return [...]components.SegmentOption{
		{ID: hitAISetFormKindOpenAI, Label: m.ProviderKindOpenAI},
		{ID: hitAISetFormKindClaude, Label: m.ProviderKindClaude},
	}
}

// aiAuthOptions is the Anthropic request-header choice. It is shown only for
// Anthropic: an OpenAI-compatible endpoint is always bearer.
func (state *State) aiAuthOptions() [2]components.SegmentOption {
	m := state.messages.AISettings
	return [...]components.SegmentOption{
		{ID: hitAISetFormAuthAPIKey, Label: m.FormAuthAPIKey},
		{ID: hitAISetFormAuthBearer, Label: m.FormAuthBearer},
	}
}

func (state *State) aiRiskOptions() [3]components.SegmentOption {
	m := state.messages.AISettings
	return [...]components.SegmentOption{
		{ID: hitAISetRiskLow, Label: m.RiskLow},
		{ID: hitAISetRiskMedium, Label: m.RiskMedium},
		{ID: hitAISetRiskHigh, Label: m.RiskHigh},
	}
}

func (state *State) aiStepsOptions() [3]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitAISetSteps6, Label: "6"}, {ID: hitAISetSteps12, Label: "12"}, {ID: hitAISetSteps24, Label: "24"},
	}
}

func (state *State) aiContextOptions() [4]components.SegmentOption {
	return [...]components.SegmentOption{
		{ID: hitAISetContext0, Label: state.messages.AISettings.ContextOff},
		{ID: hitAISetContext30, Label: "30"}, {ID: hitAISetContext60, Label: "60"}, {ID: hitAISetContext120, Label: "120"},
	}
}

func (state *State) aiMCPModeOptions() [3]components.SegmentOption {
	m := state.messages.AISettings
	return [...]components.SegmentOption{
		{ID: hitAISetMCPObserve, Label: m.MCPModeObserve},
		{ID: hitAISetMCPAsk, Label: m.MCPModeAsk},
		{ID: hitAISetMCPAllowAll, Label: m.MCPModeAllowAll},
	}
}

// aiPut stores one setting, reads everything back and tells the application.
// It reports whether the value was stored.
func (state *State) aiPut(key, value string) bool {
	db, err := state.aiStore()
	if err == nil {
		err = db.SetSetting(key, value)
	}
	if err != nil {
		state.ai.cfg.notice = state.aiScrub(err.Error(), "")
		return false
	}
	state.aiWrote()
	return true
}

// aiWrote follows every change to the database made from this page.
func (state *State) aiWrote() {
	state.reloadAI()
	state.aiClampModelPage()
	state.aiSettingsChanged()
}

// aiScrub removes an API key from text about to be shown. The libraries already
// keep keys out of their errors; this is the second lock.
func (state *State) aiScrub(message, key string) string {
	message = state.aiCLIError(message)
	if len(key) >= 4 {
		message = strings.ReplaceAll(message, key, "***")
	}
	return message
}

// aiSelectedProvider is the provider whose models are listed: the one chosen,
// or the first.
func (state *State) aiSelectedProvider() (store.Provider, bool) {
	providers := state.ai.providers
	for _, provider := range providers {
		if provider.ID == state.ai.cfg.selected {
			return provider, true
		}
	}
	if len(providers) > 0 {
		return providers[0], true
	}
	return store.Provider{}, false
}

func (state *State) aiSelectProvider(index int) {
	if index >= len(state.ai.providers) {
		return
	}
	cfg := &state.ai.cfg
	if cfg.selected != state.ai.providers[index].ID {
		cfg.selected, cfg.page, cfg.modelErr = state.ai.providers[index].ID, 0, ""
		cfg.fetched, cfg.fetchErr = false, ""
		cfg.modelInput = text.Editor{}
	}
}

func (state *State) aiProviderByID(id string) (store.Provider, bool) {
	for _, provider := range state.ai.providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return store.Provider{}, false
}

func (state *State) aiToggleProvider(index int) {
	if index >= len(state.ai.providers) {
		return
	}
	provider := state.ai.providers[index]
	provider.Enabled = !provider.Enabled
	db, err := state.aiStore()
	if err == nil {
		err = db.SaveProvider(provider)
	}
	if err != nil {
		state.ai.cfg.notice = state.aiScrub(err.Error(), provider.APIKey)
		return
	}
	state.aiWrote()
}

// aiOpenProviderForm opens the provider dialog for a new provider (id empty)
// or to edit one. The saved key is never copied into the form.
func (state *State) aiOpenProviderForm(id string) {
	cfg := &state.ai.cfg
	if _, err := state.aiStore(); err != nil {
		return
	}
	state.aiCloseDialog()
	cfg.dialog, cfg.kind, cfg.preset = aiDialogProvider, store.KindOpenAI, aiPresetCustom
	cfg.name.Restrict(printableLine, 64)
	// The importer accepts a base URL up to MaxURLRunes; the editor must not
	// silently shorten such a provider when it is opened for an unrelated edit.
	cfg.baseURL.Restrict(printableLine, aiproviderimport.MaxURLRunes)
	cfg.apiKey.Restrict(printableLine, 600)
	cfg.cliPath.Restrict(printableLine, 4096)
	if id == "" {
		return
	}
	provider, ok := state.aiProviderByID(id)
	if !ok {
		cfg.dialog = aiDialogNone
		return
	}
	cfg.editID, cfg.kind, cfg.hadKey = provider.ID, provider.Kind, provider.HasKey
	cfg.authStyle = provider.AuthStyle
	cfg.name.Insert(provider.Name)
	cfg.baseURL.Insert(provider.BaseURL)
	cfg.cliPath.Insert(provider.CLIPath)
}

// printableLine accepts what a one-line field may hold: no control characters
// and no line breaks, so a pasted key never carries a trailing newline.
func printableLine(r rune) bool { return !unicode.IsControl(r) }

// modelNameRune accepts what a model name may hold.
func modelNameRune(r rune) bool { return !unicode.IsControl(r) && !unicode.IsSpace(r) }

func newProviderID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "p" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "p" + hex.EncodeToString(b[:])
}

// validBaseURL reports whether raw is an http or https address with a host and
// no credentials in it.
func validBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func (state *State) aiSubmitProvider() {
	cfg := &state.ai.cfg
	m := state.messages.AISettings
	name := strings.TrimSpace(cfg.name.String())
	base := strings.TrimSpace(cfg.baseURL.String())
	key := strings.TrimSpace(cfg.apiKey.String())
	cliProvider := store.IsCLIKind(cfg.kind)
	if cliProvider {
		base, key = "", ""
	}
	switch {
	case name == "":
		cfg.formErr = m.ErrFormName
		return
	case !cliProvider && base != "" && !validBaseURL(base):
		cfg.formErr = m.ErrFormBaseURL
		return
	case !cliProvider && base == "" && key == "" && !cfg.hadKey:
		cfg.formErr = m.ErrFormKey
		return
	}
	provider := store.Provider{ID: cfg.editID, Name: name, Kind: cfg.kind, BaseURL: base, APIKey: key, HasKey: key != "", Enabled: true}
	if cliProvider {
		provider.CLIPath = strings.TrimSpace(cfg.cliPath.String())
	} else if cfg.kind == store.KindAnthropic {
		provider.AuthStyle = cfg.authStyle
	}
	// A new provider, or one reached differently, has models nobody has listed yet.
	refetch := cfg.editID == ""
	resetAuth := false
	if cfg.editID == "" {
		provider.ID = newProviderID()
	} else if old, ok := state.aiProviderByID(cfg.editID); ok {
		provider.Enabled = old.Enabled
		if key == "" && !cliProvider {
			provider.APIKey, provider.HasKey = old.APIKey, old.HasKey // the field is left empty to keep the key
		}
		refetch = provider.Kind != old.Kind || provider.BaseURL != old.BaseURL || provider.APIKey != old.APIKey || provider.CLIPath != old.CLIPath
		resetAuth = cliProvider && (provider.Kind != old.Kind || provider.CLIPath != old.CLIPath)
	}
	db, err := state.aiStore()
	if err == nil && key != "" {
		err = state.aiSaveKey(provider.ID, key)
	}
	if err == nil {
		err = db.SaveProvider(provider)
		if err != nil && key != "" && cfg.editID == "" {
			_ = state.secrets.vault.Delete(aiKeySecretName(provider.ID))
		}
	}
	if err != nil {
		cfg.formErr = fmt.Sprintf(m.ErrFormSaveFmt, state.aiScrub(err.Error(), key))
		return
	}
	if cfg.editID == "" {
		cfg.selected, cfg.page = provider.ID, 0
	}
	if resetAuth {
		state.aiResetCLIAuth(provider.ID)
	}
	state.aiCloseDialog()
	state.aiWrote()
	if refetch && provider.Enabled {
		cfg.selected, cfg.page = provider.ID, 0
		state.cancelAIFetch()
		state.aiFetchModels()
	}
}

// aiSaveKey puts a provider's API key into the vault.
func (state *State) aiSaveKey(providerID, key string) error {
	opened := state.secrets.vault
	if opened == nil {
		return errors.New(state.messages.VaultUnavailable)
	}
	if err := opened.Set(aiKeySecretName(providerID), key); err != nil {
		return errors.New(state.vaultErrorText(err))
	}
	return nil
}

func (state *State) aiDeleteProvider(id string) {
	if state.ai.login != nil && state.ai.login.provider.ID == id {
		state.aiStopCLILogin()
	}
	if check := state.ai.authChecks[id]; check != nil {
		check.cancel()
		delete(state.ai.authChecks, id)
	}
	cfg := &state.ai.cfg
	db, err := state.aiStore()
	if err == nil {
		// CLI providers have no API key. Asking for the master password just to
		// delete one is both surprising and unnecessary.
		provider, hasProvider := state.aiProviderByID(id)
		if opened := state.secrets.vault; hasProvider && provider.HasKey && opened != nil {
			// The key goes first, so a failure leaves the provider whole. A locked
			// vault is opened and the deletion tried again.
			switch err = opened.Delete(aiKeySecretName(id)); {
			case errors.Is(err, vault.ErrLocked):
				state.openVaultUnlock(nil, func() { state.aiDeleteProvider(id) })
				return
			case err != nil:
				err = errors.New(state.vaultErrorText(err))
			}
		}
	}
	if err == nil {
		err = db.DeleteProvider(id)
	}
	if err != nil {
		cfg.notice = state.aiScrub(err.Error(), "")
		return
	}
	if cfg.fetch != nil && cfg.fetch.providerID == id {
		state.cancelAIFetch()
	}
	if cfg.selected == id {
		cfg.selected, cfg.page, cfg.fetched, cfg.modelErr = "", 0, false, ""
	}
	state.aiWrote()
}

// aiModelsOf counts the models of a provider and how many are enabled.
func (state *State) aiModelsOf(providerID string) (total, enabled int) {
	for _, model := range state.ai.models {
		if model.ProviderID == providerID {
			total++
			if model.Enabled {
				enabled++
			}
		}
	}
	return total, enabled
}

// aiModelAt is the nth model of a provider in the stored order.
func (state *State) aiModelAt(providerID string, n int) (store.Model, bool) {
	for _, model := range state.ai.models {
		if model.ProviderID != providerID {
			continue
		}
		if n == 0 {
			return model, true
		}
		n--
	}
	return store.Model{}, false
}

func (state *State) aiClampModelPage() {
	cfg := &state.ai.cfg
	provider, ok := state.aiSelectedProvider()
	total := 0
	if ok {
		total, _ = state.aiModelsOf(provider.ID)
	}
	cfg.page = min(max(cfg.page, 0), max((total-1)/aiModelPageSize, 0))
}

// aiModelOnPage is the model shown in row of the current page.
func (state *State) aiModelOnPage(row int) (store.Provider, store.Model, bool) {
	provider, ok := state.aiSelectedProvider()
	if !ok {
		return store.Provider{}, store.Model{}, false
	}
	model, ok := state.aiModelAt(provider.ID, state.ai.cfg.page*aiModelPageSize+row)
	return provider, model, ok
}

func (state *State) aiUseModel(row int) {
	provider, model, ok := state.aiModelOnPage(row)
	if !ok {
		return
	}
	db, err := state.aiStore()
	if err == nil && !model.Enabled {
		err = db.SetModelEnabled(provider.ID, model.Name, true)
	}
	if err == nil {
		err = db.SetDefaultModel(provider.ID, model.Name)
	}
	if err != nil {
		state.ai.cfg.notice = state.aiScrub(err.Error(), provider.APIKey)
		return
	}
	state.aiWrote()
}

func (state *State) aiToggleModel(row int) {
	provider, model, ok := state.aiModelOnPage(row)
	if !ok || state.aiIsDefault(provider.ID, model.Name) {
		return // the default model stays on
	}
	db, err := state.aiStore()
	if err == nil {
		err = db.SetModelEnabled(provider.ID, model.Name, !model.Enabled)
	}
	if err != nil {
		state.ai.cfg.notice = state.aiScrub(err.Error(), provider.APIKey)
		return
	}
	state.aiWrote()
}

func (state *State) aiIsDefault(providerID, model string) bool {
	settings := &state.ai.settings
	return settings.defaultProvider == providerID && settings.defaultModel == model
}

// aiHasDefault reports whether the stored default names a model that exists.
func (state *State) aiHasDefault() bool {
	settings := &state.ai.settings
	for _, model := range state.ai.models {
		if model.ProviderID == settings.defaultProvider && model.Name == settings.defaultModel {
			return true
		}
	}
	return false
}

// aiAddModel adds the name typed in the model field to the selected provider,
// for providers that do not list their models.
func (state *State) aiAddModel() {
	cfg := &state.ai.cfg
	provider, ok := state.aiSelectedProvider()
	name := strings.TrimSpace(cfg.modelInput.String())
	if !ok || name == "" {
		return
	}
	if len([]rune(name)) > 200 || strings.ContainsFunc(name, unicode.IsSpace) {
		cfg.modelErr = state.messages.AISettings.ErrModelName
		return
	}
	cfg.modelErr = ""
	db, err := state.aiStore()
	if err != nil {
		return
	}
	models := state.aiModelList(provider.ID)
	found := false
	for index := range models {
		if models[index].Name == name {
			models[index].Enabled, found = true, true
		}
	}
	if !found {
		models = append(models, store.Model{ProviderID: provider.ID, Name: name, Enabled: true})
	}
	sortModels(models)
	if err := db.SaveModels(provider.ID, models); err != nil {
		cfg.notice = state.aiScrub(err.Error(), provider.APIKey)
		return
	}
	if !state.aiHasDefault() {
		if err := db.SetDefaultModel(provider.ID, name); err != nil {
			cfg.notice = state.aiScrub(err.Error(), provider.APIKey)
		}
	}
	cfg.modelInput = text.Editor{}
	state.aiWrote()
}

func sortModels(models []store.Model) {
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
}

// aiModelList copies the models of a provider.
func (state *State) aiModelList(providerID string) []store.Model {
	var list []store.Model
	for _, model := range state.ai.models {
		if model.ProviderID == providerID {
			list = append(list, model)
		}
	}
	return list
}

// aiFetchModels asks the selected provider which models it offers, off the
// window thread. A second press while it runs cancels it.
func (state *State) aiFetchModels() {
	cfg := &state.ai.cfg
	if cfg.fetch != nil {
		state.cancelAIFetch()
		return
	}
	provider, ok := state.aiSelectedProvider()
	if !ok {
		return
	}
	provider, ready := state.aiKeyedProvider(provider, state.aiFetchModels)
	if !ready {
		return
	}
	client := state.aiProvider(provider)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	fetch := &aiFetch{providerID: provider.ID, cancel: cancel}
	cfg.fetch, cfg.fetched, cfg.fetchErr, cfg.modelErr = fetch, false, "", ""
	key := provider.APIKey
	go func() {
		names, err := client.Models(ctx)
		state.aiPost(func(s *State) { s.aiFetchDone(fetch, names, err, key) })
	}()
}

// cancelAIFetch abandons a fetch in progress.
func (state *State) cancelAIFetch() {
	cfg := &state.ai.cfg
	if cfg.fetch != nil {
		cfg.fetch.cancel()
		cfg.fetch = nil
	}
}

func (state *State) aiFetchDone(fetch *aiFetch, names []string, err error, key string) {
	cfg := &state.ai.cfg
	if cfg.fetch != fetch {
		return // cancelled or superseded
	}
	cfg.fetch = nil
	fetch.cancel()
	if err != nil {
		cfg.fetched, cfg.fetchErr = true, state.aiScrub(err.Error(), key)
		return
	}
	if _, ok := state.aiProviderByID(fetch.providerID); !ok {
		return
	}
	// The new list replaces the old one, except what the user chose: models that
	// are enabled or are the default stay even when the provider no longer lists
	// them (a manually added model, say).
	seen := make(map[string]bool, len(names))
	var merged []store.Model
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		merged = append(merged, store.Model{ProviderID: fetch.providerID, Name: name})
		if len(merged) >= 2000 {
			break
		}
	}
	for _, old := range state.aiModelList(fetch.providerID) {
		for index := range merged {
			if merged[index].Name == old.Name {
				merged[index].Enabled = old.Enabled
			}
		}
		if !seen[old.Name] && (old.Enabled || state.aiIsDefault(old.ProviderID, old.Name)) {
			merged = append(merged, old)
		}
	}
	sortModels(merged)
	db, derr := state.aiStore()
	if derr == nil {
		derr = db.SaveModels(fetch.providerID, merged)
	}
	// With no default model yet, the assistant cannot answer: take the first
	// model that can chat, in the order the provider listed them.
	if derr == nil && !state.aiHasDefault() {
		if name := firstChatModel(names); name != "" {
			if derr = db.SetModelEnabled(fetch.providerID, name, true); derr == nil {
				derr = db.SetDefaultModel(fetch.providerID, name)
			}
		}
	}
	if derr != nil {
		cfg.fetched, cfg.fetchErr = true, state.aiScrub(derr.Error(), key)
		return
	}
	cfg.fetched, cfg.fetchCount = true, len(seen)
	state.aiWrote()
}

// firstChatModel is the first of names that is not obviously for something
// other than chat; "" when there is none.
func firstChatModel(names []string) string {
	for _, name := range names {
		name = strings.TrimSpace(name)
		lower := strings.ToLower(name)
		if name == "" {
			continue
		}
		other := false
		for _, word := range [...]string{"embed", "whisper", "tts", "dall-e", "moderation", "image", "audio", "realtime", "rerank", "transcribe", "babbage", "davinci"} {
			if strings.Contains(lower, word) {
				other = true
				break
			}
		}
		if !other {
			return name
		}
	}
	return ""
}

// aiClearHistory deletes every saved conversation and starts the panel afresh.
func (state *State) aiClearHistory() {
	cfg := &state.ai.cfg
	db, err := state.aiStore()
	if err == nil {
		err = db.DeleteAllConversations()
	}
	if err != nil {
		cfg.notice = state.aiScrub(err.Error(), "")
		return
	}
	state.ai.conversations = nil
	state.aiNewConversation()
	cfg.clearedDone = true
	state.aiSettingsChanged()
}

const aiMCPClientFormat = "claude mcp add --transport http motty %s --header \"Authorization: Bearer %s\""

// aiMCPURL is the address clients connect to.
func (state *State) aiMCPURL() string {
	if state.ai.mcpAddr == "" {
		return ""
	}
	return "http://" + state.ai.mcpAddr + mcp.Path
}

// aiCopyClientConfig copies the selected client's configuration with the live token.
func (state *State) aiCopyClientConfig() {
	state.aiCopyMCP(hitAISetMCPCopy, aiMCPConfig(state.ai.cfg.mcpClient, state.aiMCPURL(), state.ai.mcpToken))
}

func (state *State) aiMCPReady() bool {
	return state.ai.settings.mcpEnabled && state.ai.mcpAddr != "" && state.ai.mcpToken != ""
}

func (state *State) aiCopyMCP(target ui.ID, value string) {
	if !state.aiMCPReady() {
		return
	}
	state.aiCopyText(target, value)
}
