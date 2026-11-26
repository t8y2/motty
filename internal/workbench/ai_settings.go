package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"motty/internal/ai/llm"
	"motty/internal/ai/mcp"
	"motty/internal/ai/probe"
	"motty/internal/ai/reasoning"
	"motty/internal/ai/store"
	"motty/internal/aiproviderimport"
	"motty/internal/vault"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
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
	hitAISetProviderAdd     = tree.ID(aiSettingsHits, "ai-set-provider-add")
	hitAISetProviderImport  = tree.ID(aiSettingsHits, "ai-set-provider-import")
	hitAISetImportSource    = tree.Sub(aiSettingsHits, tree.Name("ai-set-import-source")) // preview, per source
	hitAISetImportChoose    = tree.ID(aiSettingsHits, "ai-set-import-choose")
	hitAISetImportBack      = tree.ID(aiSettingsHits, "ai-set-import-back")
	hitAISetImportApply     = tree.ID(aiSettingsHits, "ai-set-import-apply")
	hitAISetImportClose     = tree.ID(aiSettingsHits, "ai-set-import-close")
	hitAISetImportList      = tree.ID(aiSettingsHits, "ai-set-import-list")            // scroll state and group hit
	hitAISetImportRow       = tree.Sub(aiSettingsHits, tree.Name("ai-set-import-row")) // select, per candidate
	hitAISetFormDismiss     = tree.ID(aiSettingsHits, "ai-set-form-dismiss")
	hitAISetFormClose       = tree.ID(aiSettingsHits, "ai-set-form-close")
	hitAISetFormCancel      = tree.ID(aiSettingsHits, "ai-set-form-cancel")
	hitAISetFormSubmit      = tree.ID(aiSettingsHits, "ai-set-form-submit")
	hitAISetFormName        = tree.ID(aiSettingsHits, "ai-set-form-name")
	hitAISetFormURL         = tree.ID(aiSettingsHits, "ai-set-form-url")
	hitAISetFormModelsURL   = tree.ID(aiSettingsHits, "ai-set-form-models-url")
	hitAISetFormProxy       = tree.ID(aiSettingsHits, "ai-set-form-proxy")
	hitAISetFormKey         = tree.ID(aiSettingsHits, "ai-set-form-key")
	hitAISetFormCLIPath     = tree.ID(aiSettingsHits, "ai-set-form-cli-path")
	hitAISetConfirmCancel   = tree.ID(aiSettingsHits, "ai-set-confirm-cancel")
	hitAISetConfirmAction   = tree.ID(aiSettingsHits, "ai-set-confirm-action")
	hitAISetFetchModels     = tree.ID(aiSettingsHits, "ai-set-fetch-models")
	hitAISetModelSearch     = tree.ID(aiSettingsHits, "ai-set-model-search")
	hitAISetModelInput      = tree.ID(aiSettingsHits, "ai-set-model-input")
	hitAISetModelAdd        = tree.ID(aiSettingsHits, "ai-set-model-add")
	hitAISetModelPrev       = tree.ID(aiSettingsHits, "ai-set-model-prev")
	hitAISetModelNext       = tree.ID(aiSettingsHits, "ai-set-model-next")
	hitAISetStepsMinus      = tree.ID(aiSettingsHits, "ai-set-steps-minus")
	hitAISetStepsValue      = tree.ID(aiSettingsHits, "ai-set-steps-value")
	hitAISetStepsPlus       = tree.ID(aiSettingsHits, "ai-set-steps-plus")
	hitAISetFilterAll       = tree.ID(aiSettingsHits, "ai-set-filter-all")
	hitAISetFilterAvailable = tree.ID(aiSettingsHits, "ai-set-filter-avail")
	hitAISetFilterFailed    = tree.ID(aiSettingsHits, "ai-set-filter-failed")
	hitAISetFilterEnabled   = tree.ID(aiSettingsHits, "ai-set-filter-enabled")
	hitAISetSortDefault     = tree.ID(aiSettingsHits, "ai-set-sort-default")
	hitAISetSortLatencyAsc  = tree.ID(aiSettingsHits, "ai-set-sort-latency-asc")
	hitAISetSortLatencyDesc = tree.ID(aiSettingsHits, "ai-set-sort-latency-desc")
	hitAISetSortNameAsc     = tree.ID(aiSettingsHits, "ai-set-sort-name-asc")
	hitAISetRedact          = tree.ID(aiSettingsHits, "ai-set-redact")
	hitAISetHistory         = tree.ID(aiSettingsHits, "ai-set-history")
	hitAISetClearHistory    = tree.ID(aiSettingsHits, "ai-set-clear-history")
	hitAISetMCPEnabled      = tree.ID(aiSettingsHits, "ai-set-mcp-enabled")
	hitAISetMCPCopy         = tree.ID(aiSettingsHits, "ai-set-mcp-copy")
	hitAISetMCPCopyURL      = tree.ID(aiSettingsHits, "ai-set-mcp-copy-url")
	hitAISetMCPCopyToken    = tree.ID(aiSettingsHits, "ai-set-mcp-copy-token")
	hitAISetMCPReset        = tree.ID(aiSettingsHits, "ai-set-mcp-reset")
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
	hitAISetModelProbeAll  = tree.ID(aiSettingsHits, "ai-set-model-probe-all")
	hitAISetProbeStart     = tree.ID(aiSettingsHits, "ai-set-probe-start")
	hitAISetModelProbe     = tree.Sub(aiSettingsHits, tree.Name("ai-set-model-probe"))

	hitAISetModelReasoning  = tree.Sub(aiSettingsHits, tree.Name("ai-set-model-reasoning"))
	hitAISetReasoningSave   = tree.ID(aiSettingsHits, "ai-set-reasoning-save")
	hitAISetReasoningCancel = tree.ID(aiSettingsHits, "ai-set-reasoning-cancel")
	hitAISetReasoningClose  = tree.ID(aiSettingsHits, "ai-set-reasoning-close")
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
	aiDialogAgentAllowAll
	aiDialogProbe
	aiDialogModelReasoning
)

// aiModelFilter specifies the filtering condition for models of a provider.
type aiModelFilter uint8

const (
	aiModelFilterAll aiModelFilter = iota
	aiModelFilterAvailable
	aiModelFilterFailed
	aiModelFilterEnabled
)

// aiModelSort specifies the sort order for models of a provider.
type aiModelSort uint8

const (
	aiModelSortDefault aiModelSort = iota
	aiModelSortLatencyAsc
	aiModelSortLatencyDesc
	aiModelSortNameAsc
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
// aiFormFocus says which field of the provider form the application asks the
// focus for; zero means none. The form binds its fields to it, so an invalid
// field can take the focus after a rejected submit.
type aiFormFocus uint8

const (
	aiFormFocusNone aiFormFocus = iota
	aiFormFocusName
	aiFormFocusCLIPath
	aiFormFocusURL
	aiFormFocusModelsURL
	aiFormFocusProxy
	aiFormFocusKey
)

// dialog, the selected provider and the outcome of the last action. The saved
// choices themselves are in aiState.settings.
type aiSettingsUI struct {
	dialog aiDialogKind

	// The provider dialog. editID is empty when a provider is being added.
	editID string
	kind   string
	// preset is the entry of aiPresets the form was last filled from.
	preset           int
	name             text.Editor
	baseURL          text.Editor
	modelsURL        text.Editor
	proxyURL         text.Editor
	apiKey           text.Editor
	cliPath          text.Editor
	cliAutoPath      string
	cliPathMissing   bool
	authStyle        string // Anthropic only; empty means the API key default
	apiFormat        string // OpenAI-compatible only; empty means Chat Completions
	hadKey           bool
	formErr          string
	formErrSelection ui.TextSelectionState // what the user selected of formErr, to copy it
	formFocus        aiFormFocus
	// deleteID is the provider the delete question is about.
	deleteID string

	selected        string // provider whose models are listed
	page            int    // page of that provider's models
	modelFilter     aiModelFilter
	modelSort       aiModelSort
	modelSearch     text.Editor
	modelSearchLast string
	modelInput      text.Editor
	stepsInput      text.Editor
	// modelSearchField, modelField and stepsField are the page's handles on those
	// editors, bound once before the page declares them.
	modelSearchField, modelField, stepsField ui.Field
	// stepsEditing says the step field is being typed into; the stored setting
	// changes when the edit is committed.
	stepsEditing bool

	probeResults    map[string]map[string]probe.Result
	probeCancel     context.CancelFunc
	probingProvider string
	probingModel    string
	// probeSeq is the run a result belongs to: a probe started later makes the
	// results of the one it replaced arrive stale and get dropped.
	probeSeq         uint64
	probeProgress    [2]int
	probeConcurrency int
	probeTimeout     time.Duration

	fetch              *aiFetch
	fetched            bool // a fetch ended: with fetchCount models, or fetchErr
	fetchCount         int
	fetchErr           string
	modelErr           string
	notice             string // why the last change could not be stored
	clearedDone        bool
	copied             aiCopyState
	copyTarget         gfx.ID
	mcpClient          aiMCPClient
	mcpPreviewLines    []string
	agentClient        aiMCPClient
	agentPreviewLines  []string
	agentProgram       string
	agentDataDir       string
	agentPreviewClient aiMCPClient
	agentPreviewConfig string
	agentHostsView     agentHostsViewUI
	tokenReset         bool
	loadPosted         bool // the first load was queued from a paint

	// The provider import dialog. The candidates carry credentials until the
	// dialog closes or the import ends; only key presence is ever painted.
	importStep         aiImportStep
	importSources      []aiproviderimport.SourceInfo
	importCandidates   []aiproviderimport.Candidate
	importSelected     map[int]bool
	importErr          string
	importErrSelection ui.TextSelectionState // what the user selected of importErr, to copy it
	importResult       aiImportResult

	// The reasoning configuration dialog for a model.
	reasoningModelName  string
	reasoningProviderID string
	reasoningMode       int // 0: Auto, 1: Enabled, 2: Disabled
	reasoningPreset     int // 0: 3档(low,medium,high), 1: 6档(minimal..max), 2: 2档(default,high)

	// The form's fields share the editors the application reads. They are bound
	// once, by the page that declares them.
	formBound     bool
	formName      ui.Field
	formCLIPath   ui.Field
	formBaseURL   ui.Field
	formModelsURL ui.Field
	formProxyURL  ui.Field
	formAPIKey    ui.Field
	// proxyHadPassword says a proxy password is in the vault; leaving the proxy
	// address unchanged keeps it.
	proxyHadPassword bool
	// inputBound says the model-name and step-count fields are bound.
	inputBound bool
	// formKeyShown shows the API key instead of its bullets.
	formKeyShown bool
	// formKind, formAuth and formFormat are the forms' choices' bound values.
	formKind, formAuth, formFormat string
}

func (state *State) aiSettingsVisible() bool {
	return state.activeSection == sectionSettings && state.modal == modalNone
}

// aiSettingsOpened loads the assistant's choices when the page is opened, so
// painting never touches the disk.
func (state *State) aiSettingsOpened() {
	_, _ = state.aiStore()
	if state.ai.cfg.agentProgram == "" {
		state.ai.cfg.agentProgram = state.agentExecutable()
		state.ai.cfg.agentDataDir = filepath.Dir(state.options.KnownHostsPath)
		state.ai.cfg.agentPreviewConfig = ""
	}
}

// The step-count field accepts this range. The agent treats a zero MaxSteps as
// unlimited, but the settings page never produces that value.
const (
	aiStepsMin = 1
	aiStepsMax = 500
)

func aiClampSteps(steps int) int {
	return min(max(steps, aiStepsMin), aiStepsMax)
}

// stepDigit accepts the characters a step count may hold.
func stepDigit(r rune) bool { return r >= '0' && r <= '9' }

// newStepsInput is the step count's editor, holding value, with the digits and
// length the field accepts. An editor is restricted when it is made: clearing one
// with the zero editor would drop the restriction for the rest of the config's
// life.
func newStepsInput(value string) text.Editor {
	var editor text.Editor
	editor.Restrict(stepDigit, 3)
	editor.Insert(value)
	return editor
}

// newModelInput is an empty model name's editor with the characters and length the
// field accepts.
func newModelInput() text.Editor {
	var model text.Editor
	model.Restrict(modelNameRune, 200)
	return model
}

// newModelSearch returns an empty model search editor with single-line and length limits.
func newModelSearch() text.Editor {
	var search text.Editor
	search.Restrict(printableLine, 200)
	return search
}

// aiModelSearchQuery returns the normalized search query for the model list.
func (state *State) aiModelSearchQuery() string {
	return strings.ToLower(strings.TrimSpace(state.ai.cfg.modelSearch.String()))
}

// aiModelMatchesSearch reports whether a model's name or localized label matches
// every whitespace-separated token in query (case-insensitively).
func aiModelMatchesSearch(name, displayLabel, query string) bool {
	if query == "" {
		return true
	}
	lowerName := strings.ToLower(name)
	lowerLabel := ""
	if displayLabel != "" && displayLabel != name {
		lowerLabel = strings.ToLower(displayLabel)
	}
	for _, token := range strings.Fields(query) {
		if !strings.Contains(lowerName, token) && (lowerLabel == "" || !strings.Contains(lowerLabel, token)) {
			return false
		}
	}
	return true
}

// aiSetSteps stores a step count, clamped to the range the field accepts.
func (state *State) aiSetSteps(steps int) {
	steps = aiClampSteps(steps)
	if steps == state.ai.settings.maxSteps {
		return
	}
	state.aiPut("ai.max_steps", strconv.Itoa(steps))
}

// aiStepBy moves the step count by delta, committing a field edit first.
func (state *State) aiStepBy(delta int) {
	state.aiCommitSteps()
	state.aiSetSteps(state.ai.settings.maxSteps + delta)
}

// aiBeginStepsEdit opens the field for typing, seeded with the stored value.
func (state *State) aiBeginStepsEdit() {
	cfg := &state.ai.cfg
	if cfg.stepsEditing {
		return
	}
	cfg.stepsInput = newStepsInput(strconv.Itoa(state.ai.settings.maxSteps))
	cfg.stepsInput.SetSelection(text.Range{Start: 0, End: cfg.stepsInput.RuneCount()})
	cfg.stepsEditing = true
}

// aiCommitSteps stores what was typed in the field, if it is a number, and
// closes the edit. An empty or malformed value leaves the setting unchanged.
func (state *State) aiCommitSteps() {
	cfg := &state.ai.cfg
	if !cfg.stepsEditing {
		return
	}
	cfg.stepsEditing = false
	value := strings.TrimSpace(cfg.stepsInput.String())
	cfg.stepsInput = newStepsInput("")
	steps, err := strconv.Atoi(value)
	if err != nil {
		return
	}
	state.aiSetSteps(steps)
}

func (state *State) aiSettingsClick(id gfx.ID) bool {
	if !state.aiSettingsVisible() {
		return false
	}
	cfg := &state.ai.cfg
	if cfg.dialog == aiDialogNone && cfg.stepsEditing && id != hitAISetStepsValue && id != hitAISetStepsMinus && id != hitAISetStepsPlus {
		state.aiCommitSteps()
	}
	if cfg.dialog != aiDialogNone {
		state.aiDialogClick(id)
		return true // nothing behind a dialog is reachable
	}
	if !tree.Within(id.Scope(), aiSettingsHits) {
		return false
	}
	cfg.clearedDone, cfg.copied, cfg.tokenReset, cfg.notice = false, aiCopyIdle, false, ""
	switch {
	case id == hitAISetStepsMinus:
		state.aiStepBy(-1)
	case id == hitAISetStepsPlus:
		state.aiStepBy(1)
	case id == hitAISetStepsValue:
		state.aiBeginStepsEdit()
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
	case id == hitAISetModelProbeAll:
		if cfg.probeCancel != nil && cfg.probingProvider == cfg.selected && cfg.probingModel == "" {
			state.aiProbeAllModels()
		} else {
			cfg.dialog = aiDialogProbe
		}
	case id.Scope() == hitAISetModelProbe:
		index, _ := id.In(hitAISetModelProbe)
		state.aiProbeSingleModel(index)
	case id == hitAISetModelAdd:
		state.aiAddModel()
	case id == hitAISetModelPrev:
		cfg.page = max(cfg.page-1, 0)
	case id == hitAISetModelNext:
		cfg.page++
		state.aiClampModelPage()
	case id == hitAISetFilterAll:
		cfg.modelFilter, cfg.page = aiModelFilterAll, 0
		state.aiSettingsChanged()
	case id == hitAISetFilterAvailable:
		cfg.modelFilter, cfg.page = aiModelFilterAvailable, 0
		state.aiSettingsChanged()
	case id == hitAISetFilterFailed:
		cfg.modelFilter, cfg.page = aiModelFilterFailed, 0
		state.aiSettingsChanged()
	case id == hitAISetFilterEnabled:
		cfg.modelFilter, cfg.page = aiModelFilterEnabled, 0
		state.aiSettingsChanged()
	case id == hitAISetSortDefault:
		cfg.modelSort, cfg.page = aiModelSortDefault, 0
		state.aiSettingsChanged()
	case id == hitAISetSortLatencyAsc:
		cfg.modelSort, cfg.page = aiModelSortLatencyAsc, 0
		state.aiSettingsChanged()
	case id == hitAISetSortLatencyDesc:
		cfg.modelSort, cfg.page = aiModelSortLatencyDesc, 0
		state.aiSettingsChanged()
	case id == hitAISetSortNameAsc:
		cfg.modelSort, cfg.page = aiModelSortNameAsc, 0
		state.aiSettingsChanged()
	case id.Scope() == hitAISetModelUse:
		index, _ := id.In(hitAISetModelUse)
		state.aiUseModel(index)
	case id.Scope() == hitAISetModelSwitch:
		index, _ := id.In(hitAISetModelSwitch)
		state.aiToggleModel(index)
	case id.Scope() == hitAISetModelReasoning:
		row, _ := id.In(hitAISetModelReasoning)
		state.aiOpenModelReasoning(row)
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
	case state.aiAgentClick(id):
	case id == hitAISetMCPCopy:
		state.aiCopyClientConfig()
	case id == hitAISetMCPCopyURL:
		state.aiCopyMCP(id, state.aiMCPURL())
	case id == hitAISetMCPCopyToken:
		state.aiCopyMCP(id, state.ai.mcpToken)
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

func (state *State) aiDialogClick(id gfx.ID) {
	cfg := &state.ai.cfg
	switch cfg.dialog {
	case aiDialogCLILogin:
		if id == hitAISetFormDismiss || id == hitAISetFormClose || id == hitAISetFormCancel {
			state.aiStopCLILogin()
		}
	case aiDialogProvider:
		switch id {
		case hitAISetFormClose, hitAISetFormCancel:
			state.aiCloseDialog()
		case hitAISetFormSubmit:
			state.aiSubmitProvider()
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
	case aiDialogAgentAllowAll:
		switch id {
		case hitAISetConfirmCancel:
			state.aiCloseDialog()
		case hitAISetConfirmAction:
			if state.aiPut(agentModeSetting, AgentModeAllowAll) {
				state.aiCloseDialog()
			}
		}
	case aiDialogProbe:
		switch id {
		case hitAISetFormClose, hitAISetFormCancel:
			state.aiCloseDialog()
		case hitAISetProbeStart:
			// A probe already running is replaced, not treated as the cancel
			// the probe-all button means.
			state.aiCancelProbe()
			state.aiCloseDialog()
			state.aiProbeAllModels()
		}
	case aiDialogModelReasoning:
		switch id {
		case hitAISetFormClose, hitAISetReasoningClose, hitAISetReasoningCancel:
			state.aiCloseDialog()
		case hitAISetReasoningSave:
			state.aiSaveModelReasoning()
		}
	case aiDialogImport:
		state.aiImportDialogClick(id)
	}
}

// aiImportDialogClick handles the import dialog's three steps.
func (state *State) aiImportDialogClick(id gfx.ID) {
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
	cfg.dialog, cfg.editID, cfg.deleteID, cfg.formErr, cfg.hadKey = aiDialogNone, "", "", "", false
	cfg.formFocus = aiFormFocusNone
	cfg.notice = ""
	cfg.name, cfg.baseURL, cfg.modelsURL, cfg.proxyURL, cfg.apiKey = text.Editor{}, text.Editor{}, text.Editor{}, text.Editor{}, text.Editor{}
	cfg.cliPath, cfg.authStyle, cfg.apiFormat = text.Editor{}, "", ""
	cfg.cliAutoPath, cfg.cliPathMissing = "", false
	cfg.proxyHadPassword = false
	cfg.formKeyShown = false
	// The import candidates hold credentials in memory; closing the dialog is
	// also how they are forgotten.
	cfg.importStep, cfg.importErr = aiImportSources, ""
	cfg.importSources, cfg.importCandidates, cfg.importSelected = nil, nil, nil
	cfg.importResult = aiImportResult{}
	cfg.reasoningModelName, cfg.reasoningProviderID = "", ""
	cfg.reasoningMode, cfg.reasoningPreset = 0, 0
}

func (state *State) aiSettingsPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.ai.login != nil && event.Target == hitAICLILoginTerminal {
		return gfx.EventResult{Handled: true, Focus: hitAICLILoginTerminal}, true
	}
	// Pressing the step field selects the count as a whole and keeps the
	// runtime from placing a caret inside it: typing replaces the value.
	if state.aiSettingsVisible() && state.ai.cfg.dialog == aiDialogNone &&
		event.Kind == gfx.PointerDown && event.Target == hitAISetStepsValue &&
		(event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone) {
		state.aiBeginStepsEdit()
		return gfx.EventResult{Focus: hitAISetStepsValue, FocusVisible: true, Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) aiSettingsScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if state.ai.login != nil && state.ai.cfg.dialog == aiDialogCLILogin {
		state.ai.login.term.screen.Scroll(int(event.StepY(state.cellHeight) / max(state.cellHeight, 1)))
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) aiSettingsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.ai.login != nil && state.ai.cfg.dialog == aiDialogCLILogin {
		if event.Key == gfx.KeyEscape {
			state.aiStopCLILogin()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return state.aiCLILoginKey(event), true
	}
	if !state.aiSettingsVisible() {
		return gfx.EventResult{}, false
	}
	cfg := &state.ai.cfg
	if cfg.dialog == aiDialogMCPAllowAll || cfg.dialog == aiDialogAgentAllowAll {
		return gfx.EventResult{}, false // the alert's focus trap owns its keys
	}
	if cfg.dialog == aiDialogNone {
		if cfg.stepsEditing && event.Target != hitAISetStepsValue {
			state.aiCommitSteps()
		}
		if cfg.stepsEditing && event.Target == hitAISetStepsValue {
			if event.Key == gfx.KeyTab {
				// Leaving the field keeps what was typed, and Tab still moves on.
				state.aiCommitSteps()
			}
			switch event.Key {
			case gfx.KeyEnter:
				state.aiCommitSteps()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			case gfx.KeyEscape:
				cfg.stepsEditing, cfg.stepsInput = false, text.Editor{}
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
		if event.Target == hitAISetModelSearch && event.Key == gfx.KeyEscape {
			if _, composing := cfg.modelSearch.Composition(); composing {
				return gfx.EventResult{Handled: true}, true
			}
			if cfg.modelSearch.RuneCount() > 0 {
				cfg.modelSearch = newModelSearch()
				cfg.modelSearchLast = ""
				cfg.page = 0
				return gfx.EventResult{Handled: true, Repaint: true, Focus: hitAISetModelSearch}, true
			}
		}
	}
	if event.Key == gfx.KeyEnter && !event.Repeat {
		switch {
		case cfg.dialog == aiDialogProvider && (event.Target == hitAISetFormName || event.Target == hitAISetFormURL || event.Target == hitAISetFormModelsURL || event.Target == hitAISetFormProxy || event.Target == hitAISetFormKey || event.Target == hitAISetFormCLIPath):
			state.aiSubmitProvider()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		case cfg.dialog == aiDialogNone && event.Target == hitAISetModelInput:
			state.aiAddModel()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}

func (state *State) aiSettingsEditor(hit gfx.ID) *text.Editor {
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
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormModelsURL && !store.IsCLIKind(cfg.kind):
		return &cfg.modelsURL
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormProxy && !store.IsCLIKind(cfg.kind):
		return &cfg.proxyURL
	case cfg.dialog == aiDialogProvider && hit == hitAISetFormKey && !store.IsCLIKind(cfg.kind):
		return &cfg.apiKey
	}
	return nil
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
		cfg.modelInput = newModelInput()
		cfg.modelSearch, cfg.modelSearchLast = newModelSearch(), ""
		cfg.modelFilter, cfg.modelSort = aiModelFilterAll, aiModelSortDefault
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
	cfg.formFocus = aiFormFocusNone
	cfg.name.Restrict(printableLine, 64)
	// The importer accepts a base URL up to MaxURLRunes; the editor must not
	// silently shorten such a provider when it is opened for an unrelated edit.
	cfg.baseURL.Restrict(printableLine, aiproviderimport.MaxURLRunes)
	cfg.modelsURL.Restrict(printableLine, aiproviderimport.MaxURLRunes)
	cfg.proxyURL.Restrict(printableLine, aiproviderimport.MaxURLRunes)
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
	cfg.proxyHadPassword = provider.HasProxyPassword
	cfg.authStyle, cfg.apiFormat = provider.AuthStyle, provider.APIFormat
	cfg.name.Insert(provider.Name)
	cfg.baseURL.Insert(provider.BaseURL)
	cfg.modelsURL.Insert(provider.ModelsURL)
	cfg.proxyURL.Insert(provider.ProxyURL)
	cfg.cliPath.Insert(provider.CLIPath)
	state.aiDiscoverCLIPath()
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
	modelsURL := strings.TrimSpace(cfg.modelsURL.String())
	proxyRaw := strings.TrimSpace(cfg.proxyURL.String())
	key := strings.TrimSpace(cfg.apiKey.String())
	cliProvider := store.IsCLIKind(cfg.kind)
	proxyAddress, proxyPassword, proxyOK := llm.ParseProxyURL(proxyRaw)
	if cliProvider {
		base, modelsURL, proxyRaw, key = "", "", "", ""
		proxyAddress, proxyPassword, proxyOK = "", "", true
	}
	switch {
	case name == "":
		cfg.formErr = m.ErrFormName
		cfg.formFocus = aiFormFocusName
		return
	case !cliProvider && base != "" && !validBaseURL(base):
		cfg.formErr = m.ErrFormBaseURL
		cfg.formFocus = aiFormFocusURL
		return
	case !cliProvider && modelsURL != "" && !validBaseURL(modelsURL):
		cfg.formErr = m.ErrFormModelsURL
		cfg.formFocus = aiFormFocusModelsURL
		return
	case !cliProvider && !proxyOK:
		cfg.formErr = m.ErrFormProxyURL
		cfg.formFocus = aiFormFocusProxy
		return
	case !cliProvider && base == "" && key == "" && !cfg.hadKey:
		cfg.formErr = m.ErrFormKey
		cfg.formFocus = aiFormFocusKey
		return
	}
	provider := store.Provider{
		ID: cfg.editID, Name: name, Kind: cfg.kind, BaseURL: base, ModelsURL: modelsURL,
		ProxyURL: proxyAddress, APIKey: key, HasKey: key != "", Enabled: true,
	}
	if cliProvider {
		provider.CLIPath = strings.TrimSpace(cfg.cliPath.String())
		if provider.CLIPath == cfg.cliAutoPath {
			provider.CLIPath = ""
		}
	} else if cfg.kind == store.KindAnthropic {
		provider.AuthStyle = cfg.authStyle
	} else {
		provider.APIFormat = cfg.apiFormat
	}
	// A new provider, or one reached differently, has models nobody has listed yet.
	refetch := cfg.editID == ""
	resetAuth := false
	old, editing := store.Provider{}, false
	if cfg.editID == "" {
		provider.ID = newProviderID()
	} else if previous, ok := state.aiProviderByID(cfg.editID); ok {
		old, editing = previous, true
		provider.Enabled = old.Enabled
		if key == "" && !cliProvider {
			provider.APIKey, provider.HasKey = old.APIKey, old.HasKey // the field is left empty to keep the key
		}
		refetch = provider.Kind != old.Kind || provider.BaseURL != old.BaseURL || provider.ModelsURL != old.ModelsURL ||
			provider.ProxyURL != old.ProxyURL || provider.APIKey != old.APIKey || provider.CLIPath != old.CLIPath
		resetAuth = cliProvider && (provider.Kind != old.Kind || provider.CLIPath != old.CLIPath)
	}
	// A proxy password is a secret like the API key: the URL keeps only the
	// address and user name, and the password goes to the vault.
	var proxyChanges []vault.Change
	if proxyPassword != "" {
		provider.HasProxyPassword = true
		proxyChanges = append(proxyChanges, vault.Change{Name: aiProxyPasswordSecretName(provider.ID), Value: proxyPassword})
	} else if editing && proxyAddress == old.ProxyURL {
		provider.HasProxyPassword = old.HasProxyPassword
	} else if editing && old.HasProxyPassword {
		proxyChanges = append(proxyChanges, vault.Change{Name: aiProxyPasswordSecretName(provider.ID), Delete: true})
	}
	db, err := state.aiStore()
	if err == nil {
		var changes []vault.Change
		if key != "" {
			changes = append(changes, vault.Change{Name: aiKeySecretName(provider.ID), Value: key})
		}
		changes = append(changes, proxyChanges...)
		commit := func() error { return db.SaveProvider(provider) }
		switch {
		case len(changes) == 0:
			err = commit()
		case state.secrets.vault == nil:
			err = errors.New(state.messages.VaultUnavailable)
		default:
			err = state.secrets.vault.ApplyWithCommit(changes, commit)
		}
	}
	if err != nil {
		message := state.aiScrub(err.Error(), key)
		if proxyPassword != "" {
			message = strings.ReplaceAll(message, proxyPassword, "***")
		}
		cfg.formErr = fmt.Sprintf(m.ErrFormSaveFmt, message)
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

// deleteSecretsUntilError deletes the named secrets in order and stops at the
// first failure, so that a later success cannot hide it.
func deleteSecretsUntilError(names []string, del func(string) error) error {
	for _, name := range names {
		if err := del(name); err != nil {
			return err
		}
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
		// CLI providers have no API key or proxy password. Asking for the master
		// password just to delete one is both surprising and unnecessary.
		provider, hasProvider := state.aiProviderByID(id)
		if opened := state.secrets.vault; hasProvider && opened != nil {
			var names []string
			if provider.HasKey {
				names = append(names, aiKeySecretName(id))
			}
			if provider.HasProxyPassword {
				names = append(names, aiProxyPasswordSecretName(id))
			}
			// The secrets go first, so a failure leaves the provider whole. A
			// locked vault is opened and the deletion tried again.
			switch err = deleteSecretsUntilError(names, opened.Delete); {
			case errors.Is(err, vault.ErrNeedsConversion):
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
	if cfg.probingProvider == id {
		state.aiCancelProbe()
	}
	if cfg.selected == id {
		cfg.selected, cfg.page, cfg.fetched, cfg.modelErr = "", 0, false, ""
		cfg.modelSearch, cfg.modelSearchLast = newModelSearch(), ""
	}
	state.aiWrote()
}

// aiModelsOf counts the models of a provider and how many are enabled,
// excluding non-chat models.
func (state *State) aiModelsOf(providerID string) (total, enabled int) {
	for _, model := range state.ai.models {
		if model.ProviderID == providerID && probe.IsChatOrReasoningModel(model.Name) {
			total++
			if model.Enabled {
				enabled++
			}
		}
	}
	return total, enabled
}

// aiModelMatchesQuery reports whether model matches the normalized search query,
// including the localized display label for CLI default models.
func (state *State) aiModelMatchesQuery(isCLI bool, model store.Model, query string) bool {
	if query == "" {
		return true
	}
	label := model.Name
	if isCLI && model.Name == "default" {
		label = state.messages.AISettings.CLIDefaultModel
	}
	return aiModelMatchesSearch(model.Name, label, query)
}

// aiModelCounts reports the total, available, failed and enabled model counts
// within the current search query.
func (state *State) aiModelCounts(providerID string) (total, available, failed, enabled int) {
	query := state.aiModelSearchQuery()
	isCLI := false
	if query != "" {
		provider, _ := state.aiProviderByID(providerID)
		isCLI = store.IsCLIKind(provider.Kind)
	}
	for _, model := range state.ai.models {
		if model.ProviderID == providerID && probe.IsChatOrReasoningModel(model.Name) {
			if !state.aiModelMatchesQuery(isCLI, model, query) {
				continue
			}
			total++
			if model.Enabled {
				enabled++
			}
			if res, ok := state.aiGetProbeResult(providerID, model.Name); ok {
				if res.Status == probe.StatusSuccess {
					available++
				} else if res.Status == probe.StatusFailed {
					failed++
				}
			}
		}
	}
	return total, available, failed, enabled
}

// aiAllChatModels returns all chat/reasoning models of a provider, ignoring UI filters.
func (state *State) aiAllChatModels(providerID string) []store.Model {
	var list []store.Model
	for _, model := range state.ai.models {
		if model.ProviderID == providerID && probe.IsChatOrReasoningModel(model.Name) {
			list = append(list, model)
		}
	}
	return list
}

func (state *State) aiFilteredModels(providerID string) []store.Model {
	cfg := &state.ai.cfg

	var base []store.Model
	for _, model := range state.ai.models {
		if model.ProviderID == providerID && probe.IsChatOrReasoningModel(model.Name) {
			base = append(base, model)
		}
	}
	if len(base) == 0 {
		return nil
	}

	// 1. Filter by probe or enabled status
	var filtered []store.Model
	switch cfg.modelFilter {
	case aiModelFilterAvailable:
		for _, m := range base {
			if res, ok := state.aiGetProbeResult(providerID, m.Name); ok && res.Status == probe.StatusSuccess {
				filtered = append(filtered, m)
			}
		}
	case aiModelFilterFailed:
		for _, m := range base {
			if res, ok := state.aiGetProbeResult(providerID, m.Name); ok && res.Status == probe.StatusFailed {
				filtered = append(filtered, m)
			}
		}
	case aiModelFilterEnabled:
		for _, m := range base {
			if m.Enabled {
				filtered = append(filtered, m)
			}
		}
	default:
		filtered = append([]store.Model(nil), base...)
	}

	// 1.5 Filter by search query
	if query := state.aiModelSearchQuery(); query != "" {
		provider, _ := state.aiProviderByID(providerID)
		isCLI := store.IsCLIKind(provider.Kind)
		matched := filtered[:0]
		for _, m := range filtered {
			if state.aiModelMatchesQuery(isCLI, m, query) {
				matched = append(matched, m)
			}
		}
		filtered = matched
	}

	// 2. Sort
	switch cfg.modelSort {
	case aiModelSortLatencyAsc:
		sort.SliceStable(filtered, func(i, j int) bool {
			resI, hasI := state.aiGetProbeResult(providerID, filtered[i].Name)
			resJ, hasJ := state.aiGetProbeResult(providerID, filtered[j].Name)
			succI := hasI && resI.Status == probe.StatusSuccess
			succJ := hasJ && resJ.Status == probe.StatusSuccess

			if succI != succJ {
				return succI
			}
			if succI && succJ {
				if resI.Latency != resJ.Latency {
					return resI.Latency < resJ.Latency
				}
			}
			return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
		})
	case aiModelSortLatencyDesc:
		sort.SliceStable(filtered, func(i, j int) bool {
			resI, hasI := state.aiGetProbeResult(providerID, filtered[i].Name)
			resJ, hasJ := state.aiGetProbeResult(providerID, filtered[j].Name)
			succI := hasI && resI.Status == probe.StatusSuccess
			succJ := hasJ && resJ.Status == probe.StatusSuccess

			if succI != succJ {
				return succI
			}
			if succI && succJ {
				if resI.Latency != resJ.Latency {
					return resI.Latency > resJ.Latency
				}
			}
			return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
		})
	case aiModelSortNameAsc:
		sort.SliceStable(filtered, func(i, j int) bool {
			return strings.ToLower(filtered[i].Name) < strings.ToLower(filtered[j].Name)
		})
	default:
		// aiModelSortDefault preserves original order
	}

	// 3. Pin Default Model to top if present in filtered results
	defIdx := -1
	for idx, m := range filtered {
		if state.aiIsDefault(providerID, m.Name) {
			defIdx = idx
			break
		}
	}
	if defIdx > 0 {
		defModel := filtered[defIdx]
		copy(filtered[1:defIdx+1], filtered[0:defIdx])
		filtered[0] = defModel
	}

	return filtered
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
		total = len(state.aiFilteredModels(provider.ID))
	}
	cfg.page = min(max(cfg.page, 0), max((total-1)/aiModelPageSize, 0))
}

// aiModelOnPage is the model shown in row of the current page.
func (state *State) aiModelOnPage(row int) (store.Provider, store.Model, bool) {
	provider, ok := state.aiSelectedProvider()
	if !ok {
		return store.Provider{}, store.Model{}, false
	}
	filtered := state.aiFilteredModels(provider.ID)
	index := state.ai.cfg.page*aiModelPageSize + row
	if index < 0 || index >= len(filtered) {
		return store.Provider{}, store.Model{}, false
	}
	return provider, filtered[index], true
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
	// The list shows the models that can chat, so a name it would hide cannot be
	// added either; otherwise the model would vanish the moment it was saved.
	if !probe.IsChatOrReasoningModel(name) {
		cfg.modelErr = state.messages.AISettings.ErrModelNotChat
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
	cfg.modelInput = newModelInput()
	if query := state.aiModelSearchQuery(); query != "" && !aiModelMatchesSearch(name, "", query) {
		cfg.modelSearch = newModelSearch()
		cfg.modelSearchLast = ""
		cfg.page = 0
	}
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
		if name == "" || seen[name] || !probe.IsChatOrReasoningModel(name) {
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
		// What the user chose stays stored even when the list no longer shows
		// it: a non-chat model saved by an earlier version is hidden from the
		// list, not deleted from the database here.
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

// firstChatModel is the first of names the model list would keep; "" when there
// is none. It shares probe's classification with the list, so a name that is
// offered is a name that can become the default.
func firstChatModel(names []string) string {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || !probe.IsChatOrReasoningModel(name) {
			continue
		}
		return name
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

func (state *State) aiCopyMCP(target gfx.ID, value string) {
	if !state.aiMCPReady() {
		return
	}
	state.aiCopyText(target, value)
}

// ── Model Probing ──

func (state *State) aiGetProbeResult(providerID, modelName string) (probe.Result, bool) {
	cfg := &state.ai.cfg
	if cfg.probeResults == nil {
		return probe.Result{}, false
	}
	pm, ok := cfg.probeResults[providerID]
	if !ok {
		return probe.Result{}, false
	}
	res, ok := pm[modelName]
	return res, ok
}

func (state *State) aiSetProbeResult(providerID, modelName string, res probe.Result) {
	cfg := &state.ai.cfg
	if cfg.probeResults == nil {
		cfg.probeResults = make(map[string]map[string]probe.Result)
	}
	pm, ok := cfg.probeResults[providerID]
	if !ok {
		pm = make(map[string]probe.Result)
		cfg.probeResults[providerID] = pm
	}
	pm[modelName] = res
}

func (state *State) aiIsProbing(providerID, modelName string) bool {
	cfg := &state.ai.cfg
	if cfg.probeCancel == nil || cfg.probingProvider != providerID {
		return false
	}
	if cfg.probingModel != "" {
		return cfg.probingModel == modelName
	}
	res, ok := state.aiGetProbeResult(providerID, modelName)
	return ok && res.Status == probe.StatusRunning
}

func (state *State) aiCancelProbe() {
	cfg := &state.ai.cfg
	if cfg.probeCancel != nil {
		cfg.probeCancel()
		cfg.probeCancel = nil
		cfg.probingProvider = ""
		cfg.probingModel = ""
		cfg.probeProgress = [2]int{0, 0}
	}
}

func (state *State) aiProbeConcurrency() int {
	if state.ai.cfg.probeConcurrency <= 0 {
		return 4
	}
	return state.ai.cfg.probeConcurrency
}

func (state *State) aiProbeTimeout() time.Duration {
	if state.ai.cfg.probeTimeout <= 0 {
		return 15 * time.Second
	}
	return state.ai.cfg.probeTimeout
}

func (state *State) aiProbeSingleModel(row int) {
	provider, model, ok := state.aiModelOnPage(row)
	if !ok || store.IsCLIKind(provider.Kind) {
		return
	}
	cfg := &state.ai.cfg
	if cfg.probeCancel != nil {
		state.aiCancelProbe()
	}
	provider, ready := state.aiKeyedProvider(provider, func() { state.aiProbeSingleModel(row) })
	if !ready {
		return
	}
	client := state.aiProvider(provider)
	ctx, cancel := context.WithCancel(context.Background())
	cfg.probeCancel = cancel
	cfg.probingProvider = provider.ID
	cfg.probingModel = model.Name
	cfg.probeSeq++
	seq := cfg.probeSeq
	timeout := state.aiProbeTimeout()
	state.aiSetProbeResult(provider.ID, model.Name, probe.Result{Status: probe.StatusRunning, Timestamp: time.Now()})
	state.aiSettingsChanged()

	go func() {
		res := probe.ProbeModel(ctx, provider, model.Name, client, timeout)
		state.aiPost(func(s *State) {
			if s.ai.cfg.probeSeq != seq {
				return
			}
			s.aiSetProbeResult(provider.ID, model.Name, res)
			if res.Status == probe.StatusFailed && res.Error != "" {
				s.ai.cfg.modelErr = fmt.Sprintf("%s: %s", model.Name, res.Error)
			}
			if s.ai.cfg.probeCancel != nil && s.ai.cfg.probingModel == model.Name {
				s.ai.cfg.probeCancel = nil
				s.ai.cfg.probingProvider = ""
				s.ai.cfg.probingModel = ""
			}
			s.aiSettingsChanged()
		})
	}()
}

func (state *State) aiProbeAllModels() {
	cfg := &state.ai.cfg
	if cfg.probeCancel != nil {
		state.aiCancelProbe()
		state.aiSettingsChanged()
		return
	}
	provider, ok := state.aiSelectedProvider()
	if !ok || store.IsCLIKind(provider.Kind) {
		return
	}
	provider, ready := state.aiKeyedProvider(provider, state.aiProbeAllModels)
	if !ready {
		return
	}
	models := state.aiAllChatModels(provider.ID)
	if len(models) == 0 {
		return
	}
	client := state.aiProvider(provider)
	ctx, cancel := context.WithCancel(context.Background())
	cfg.probeCancel = cancel
	cfg.probingProvider = provider.ID
	cfg.probingModel = ""
	cfg.probeSeq++
	seq := cfg.probeSeq
	cfg.probeProgress = [2]int{0, len(models)}
	timeout := state.aiProbeTimeout()
	concurrency := state.aiProbeConcurrency()

	for _, m := range models {
		state.aiSetProbeResult(provider.ID, m.Name, probe.Result{Status: probe.StatusRunning, Timestamp: time.Now()})
	}
	state.aiSettingsChanged()

	go func() {
		type job struct {
			model store.Model
		}
		jobs := make(chan job, len(models))
		for _, m := range models {
			jobs <- job{model: m}
		}
		close(jobs)

		workerCount := min(concurrency, len(models))
		doneCh := make(chan struct{}, len(models))

		for w := 0; w < workerCount; w++ {
			go func() {
				for j := range jobs {
					if ctx.Err() != nil {
						doneCh <- struct{}{}
						continue
					}
					res := probe.ProbeModel(ctx, provider, j.model.Name, client, timeout)
					state.aiPost(func(s *State) {
						if s.ai.cfg.probeSeq != seq {
							return
						}
						s.aiSetProbeResult(provider.ID, j.model.Name, res)
						s.ai.cfg.probeProgress[0]++
						s.aiSettingsChanged()
					})
					doneCh <- struct{}{}
				}
			}()
		}

		for i := 0; i < len(models); i++ {
			<-doneCh
		}

		state.aiPost(func(s *State) {
			if s.ai.cfg.probeSeq != seq {
				return
			}
			if s.ai.cfg.probeCancel != nil && s.ai.cfg.probingModel == "" {
				s.ai.cfg.probeCancel = nil
				s.ai.cfg.probingProvider = ""
			}
			var succCount int
			for _, m := range models {
				if res, ok := s.aiGetProbeResult(provider.ID, m.Name); ok && res.Status == probe.StatusSuccess {
					succCount++
				}
			}
			if succCount > 0 && s.ai.cfg.modelSort == aiModelSortDefault {
				s.ai.cfg.modelSort = aiModelSortLatencyAsc
				s.ai.cfg.page = 0
			}
			s.aiClampModelPage()
			s.aiSettingsChanged()
		})
	}()
}

// aiOpenModelReasoning 打开指定行模型的思考/推理努力配置对话框。
func (state *State) aiOpenModelReasoning(row int) {
	provider, model, ok := state.aiModelOnPage(row)
	if !ok {
		return
	}
	cfg := &state.ai.cfg
	cfg.reasoningProviderID = provider.ID
	cfg.reasoningModelName = model.Name

	if model.ReasoningDisabled {
		cfg.reasoningMode = 2 // Disabled
		cfg.reasoningPreset = 0
	} else if model.Reasoning != nil && len(model.Reasoning.Levels) > 0 {
		cfg.reasoningMode = 1 // Enabled
		switch len(model.Reasoning.Levels) {
		case 6:
			cfg.reasoningPreset = 1
		case 2:
			cfg.reasoningPreset = 2
		default:
			cfg.reasoningPreset = 0
		}
	} else {
		cfg.reasoningMode = 0 // Auto
		cfg.reasoningPreset = 0
	}
	cfg.dialog = aiDialogModelReasoning
}

// aiSaveModelReasoning 保存当前模型的思考配置至数据库并重载状态。
func (state *State) aiSaveModelReasoning() {
	cfg := &state.ai.cfg
	if cfg.reasoningProviderID == "" || cfg.reasoningModelName == "" {
		state.aiCloseDialog()
		return
	}
	var (
		reasoningCfg *store.ReasoningConfig
		disabled     bool
	)
	switch cfg.reasoningMode {
	case 0: // Auto (清除覆写，恢复代际与命名自动推断)
		reasoningCfg = nil
		disabled = false
	case 1: // Enabled (手动强制开启)
		disabled = false
		switch cfg.reasoningPreset {
		case 1:
			reasoningCfg = &store.ReasoningConfig{
				Levels:  []string{reasoning.LevelMinimal, reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh, reasoning.LevelXHigh, reasoning.LevelMax},
				Default: reasoning.LevelMedium,
			}
		case 2:
			reasoningCfg = &store.ReasoningConfig{
				Levels:  []string{reasoning.LevelDefault, reasoning.LevelHigh},
				Default: reasoning.LevelHigh,
			}
		default:
			reasoningCfg = &store.ReasoningConfig{
				Levels:  []string{reasoning.LevelLow, reasoning.LevelMedium, reasoning.LevelHigh},
				Default: reasoning.LevelMedium,
			}
		}
	case 2: // Disabled (强制禁用)
		reasoningCfg = nil
		disabled = true
	}

	db, err := state.aiStore()
	if err == nil {
		err = db.SetModelReasoning(cfg.reasoningProviderID, cfg.reasoningModelName, reasoningCfg, disabled)
	}
	if err != nil {
		cfg.formErr = err.Error()
		return
	}
	state.aiWrote()
	state.aiCloseDialog()
}
