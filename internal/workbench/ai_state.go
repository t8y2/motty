package workbench

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"motty/internal/ai/agent"
	"motty/internal/ai/attach"
	"motty/internal/ai/cli"
	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/reasoning"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
)

// aiItemKind is what one row of the assistant's conversation is.
type aiItemKind uint8

const (
	aiUser aiItemKind = iota
	aiAssistant
	aiReasoning
	aiTool
	aiNotice       // something the application says, not the model
	aiTargetChange // the primary connection changed between questions
)

// aiToolStatus is where a tool call stands.
type aiToolStatus uint8

const (
	aiToolRunning aiToolStatus = iota
	aiToolWaiting              // waiting for the user's approval
	aiToolDone
	aiToolFailed
	aiToolDenied
)

// aiItem is one row: a message, a model's reasoning, a tool call with its
// outcome, or a notice.
type aiItem struct {
	targetLabel string // connection snapshot; never inferred from the current tab
	targetKey   string // comparison only; never resolves a persisted session
	kind        aiItemKind
	text        string
	call        llm.ToolCall
	status      aiToolStatus
	result      string
	risk        risk.Level
	// attachments are the files a user message was sent with, by name.
	attachments []aiAttachmentRef
}

// aiApproval is a question to the user from a tool call that needs a yes.
type aiApproval struct {
	request gate.Request
	reply   chan aiAnswer
	// escalation is Ask mode's request to switch to Agent. It runs nothing and
	// has no reply channel; the answer starts a new run instead.
	escalation bool
}

type aiAnswer struct {
	approved bool
	text     string
}

// aiRun is one model leg in flight. An Ask escalation and its retry share
// request-level statistics across two legs.
type aiRun struct {
	stats      aiRunStats
	usageBase  llm.Usage
	target     gate.SessionInfo
	extras     []gate.SessionInfo
	targetKey  string
	cancel     context.CancelFunc
	providerID string
	// attachments are what the question in flight was sent with, to be saved
	// with it.
	attachments []aiAttachmentRef
	// open is the item that streamed text is appended to, or -1.
	open int
	// text accumulates the open item's streamed text. A builder (rather than
	// +=) keeps a long answer linear: item.text is a zero-copy view of the
	// builder's buffer and stays valid across later appends.
	text *strings.Builder
}

// aiSettings are the assistant's choices, kept in the AI database.
type aiSettings struct {
	autoUpTo     risk.Level
	maxSteps     int
	contextLines int
	maxTextBytes int
	redact       bool
	keepHistory  bool
	// External access (MCP): whether it is enabled and the shared access level.
	mcpEnabled bool
	mcpMode    gate.Mode
	// agentMode is the independent access level of the stdio server.
	agentMode gate.Mode
	// defaultProvider and defaultModel are the model new questions go to.
	defaultProvider, defaultModel string
}

func defaultAISettings() aiSettings {
	return aiSettings{autoUpTo: risk.Low, maxSteps: 12, contextLines: 60, maxTextBytes: attach.MaxTextBytes, redact: true, keepHistory: true, mcpMode: gate.ModeObserve, agentMode: gate.ModeObserve}
}

// aiState is everything the assistant keeps in a window. It costs nothing until
// the panel is first opened: no database, no goroutine, no request.
type aiState struct {
	targetNamespace string // distinguishes stored context across window lifetimes
	target          gate.SessionInfo
	// extras are the other connections ticked in the selector. They go with every
	// question the way @references do, and are only meaningful beside a target.
	extras    []gate.SessionInfo
	targetSet bool // false only while a new conversation follows the visible terminal
	inbox     *aiInbox
	drained   []func(*State)
	backend   *aiBackend
	jobs      map[string]*aiJob // background terminal commands, by id

	open bool
	mode agent.Mode
	// autoUpTo is the highest risk this conversation runs without asking, when
	// the user picked a scope from the chip. Nil follows settings.autoUpTo. The
	// pick belongs to the conversation and is never stored: a restart or a new
	// conversation goes back to the setting.
	autoUpTo *risk.Level
	// retry says the next aiSend continues the existing history, whose last
	// message is the user question, instead of adding a second user bubble. An
	// Ask-mode escalation sets it when the user approves the switch.
	retry bool
	// answerOnly makes the retried Ask run tool-less after the user declined a
	// switch to Agent, so the model can still answer instead of stopping at the
	// escalation card.
	answerOnly bool
	needLogin  string
	// panelWidth is the width the user dragged the docked panel to, 0 for the
	// default; composerHeight likewise for the message box, 0 for as tall as its
	// text asks.
	panelWidth     float32
	composerHeight float32
	items          []aiItem
	history        []llm.Message
	conv           int64
	input          text.Editor
	// attachments wait in the message box for the next question; attaching is how
	// many files are still being read for it.
	attachments []aiAttachment
	attaching   int
	pasted      int         // pictures pasted so far, which numbers their names
	pastedText  int         // text snippets pasted so far, which numbers their names
	confirm     text.Editor // the word a critical action asks to be typed
	run         *aiRun
	approvals   []*aiApproval
	// scroll is the conversation's scroll policy: it keeps the newest row in view
	// while the reader is at the end and leaves a reader who scrolled up alone.
	scroll components.MessageScrollState
	notice string // why the last send did nothing

	// lastUsage is the token accounting of the finished run, totalUsage the
	// running sum over this conversation while the window is open. The
	// provider does not always report usage, so zeros mean unknown.
	lastUsage  llm.Usage
	totalUsage llm.Usage
	lastRun    *aiRunStats
	// pendingRun is the request-level state retained after an Ask leg requests
	// Agent mode. The approval wait and either retry remain one timed request.
	pendingRun *aiRunStats
	// rename is the conversation-rename dialog's state.
	rename aiRenameDialog

	store      *store.Store
	storeErr   error
	storeTried bool
	settings   aiSettings
	providers  []store.Provider
	models     []store.Model
	// list of saved conversations for the history menu, loaded on demand.
	conversations []store.Conversation

	// mcpAddr and mcpToken are where external agents reach the application and
	// what they must present; empty while the server is not running.
	mcpAddr, mcpToken string
	// agentAudit is the latest calls agents made without the window.
	agentAudit []store.AuditEntry
	// agentSummary caches what the page shows about them.
	agentSummary agentSummary
	// cfg is the settings section's own state (ai_settings.go).
	cfg        aiSettingsUI
	login      *aiCLILogin
	auth       map[string]cli.AuthState
	authChecks map[string]*aiCLIAuthCheck
	// panel is the assistant panel's own state (ai_panel.go).
	panel aiPanel

	// Cached reasoning effort state for current model to avoid per-frame allocations during paint.
	reasoningCachedModel    string
	reasoningCachedConfig   *store.ReasoningConfig
	reasoningCachedDisabled bool
	reasoningCachedLevels   []string
	reasoningCachedOK       bool
}

func (state *State) initAI() {
	state.ai.inbox = &aiInbox{}
	state.ai.backend = &aiBackend{state: state}
	state.ai.settings = defaultAISettings()
	state.ai.scroll = components.MessageScrollState{AutoScroll: true}
	state.ai.scroll.ScrollToEnd()
}

func (state *State) closeAI() {
	if state.ai.inbox == nil {
		return
	}
	if state.ai.run != nil {
		state.ai.run.cancel()
	}
	state.aiStopCLILogin()
	for _, check := range state.ai.authChecks {
		check.cancel()
	}
	for _, approval := range state.ai.approvals {
		if approval.reply != nil {
			approval.reply <- aiAnswer{}
		}
	}
	state.ai.approvals = nil
	state.cancelAIFetch()
	state.ai.inbox.close()
	state.ai.backend.closeAll()
	if state.ai.store != nil {
		_ = state.ai.store.Close()
		state.ai.store = nil
	}
}

// aiStore opens the AI database the first time it is needed. A failure is
// remembered and shown, not retried on every paint.
func (state *State) aiStore() (*store.Store, error) {
	ai := &state.ai
	if ai.store != nil || ai.storeTried {
		return ai.store, ai.storeErr
	}
	ai.storeTried = true
	path := state.options.AIStorePath
	if path == "" {
		path = filepath.Join(filepath.Dir(state.options.StatePath), "ai.db")
	}
	opened, err := store.Open(path)
	if err != nil {
		ai.storeErr = err
		return nil, err
	}
	ai.store = opened
	state.adoptRecordedProviders(opened)
	state.reloadAI()
	return opened, nil
}

// reloadAI reads providers, models and settings from the database.
func (state *State) reloadAI() {
	ai := &state.ai
	if ai.store == nil {
		return
	}
	ai.providers, _ = ai.store.Providers()
	state.aiAdoptKeys()
	ai.models, _ = ai.store.Models()
	state.reloadAISettings()
	read := func(key string, apply func(string)) {
		if value, ok, err := ai.store.Setting(key); err == nil && ok {
			apply(value)
		}
	}
	ai.agentAudit, _ = ai.store.Audit(agentAuditShown)
	ai.agentSummary.lines = nil
	read("ai.mode", func(v string) {
		if v == "agent" {
			ai.mode = agent.ModeAgent
		} else {
			ai.mode = agent.ModeAsk
		}
	})
	// The providers changed, or were just read: the records follow.
	state.save()
}

// reloadAISettings refreshes choices shared by already-open windows. It reads
// only an opened store and never saves or changes a conversation, a running
// request, or a settings dialog's draft.
func (state *State) reloadAISettings() {
	ai := &state.ai
	if ai.store == nil {
		return
	}
	settings := defaultAISettings()
	failed := false
	read := func(key string, apply func(string)) {
		value, ok, err := ai.store.Setting(key)
		if err != nil {
			failed = true
		} else if ok {
			apply(value)
		}
	}
	read("ai.auto_up_to", func(v string) {
		switch v {
		case "medium":
			settings.autoUpTo = risk.Medium
		case "high":
			settings.autoUpTo = risk.High
		default:
			settings.autoUpTo = risk.Low
		}
	})
	read("ai.max_steps", func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 50 {
			settings.maxSteps = n
		}
	})
	read("ai.context_lines", func(v string) {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 500 {
			settings.contextLines = n
		}
	})
	read("ai.redact", func(v string) { settings.redact = v != "off" })
	read("ai.attachment_text_mib", func(v string) {
		switch v {
		case "1", "4", "8":
			settings.maxTextBytes, _ = strconv.Atoi(v)
			settings.maxTextBytes <<= 20
		}
	})
	read("ai.history", func(v string) { settings.keepHistory = v != "off" })
	read("ai.mcp.enabled", func(v string) { settings.mcpEnabled = v == "on" })
	read("ai.mcp.mode", func(v string) { settings.mcpMode = mcpModeFromSetting(v) })
	read(agentModeSetting, func(v string) { settings.agentMode = mcpModeFromSetting(v) })
	// Export client choices live beside the settings in ai.db but not in
	// aiSettings: they change which configuration the page shows, not what the
	// assistant or the MCP servers do.
	read(aiMCPClientSetting, func(v string) { ai.cfg.mcpClient = aiMCPClientFromKey(v) })
	read(aiAgentClientSetting, func(v string) { ai.cfg.agentClient = aiMCPClientFromKey(v) })
	if provider, model, ok, err := ai.store.DefaultModel(); err != nil {
		failed = true
	} else if ok {
		settings.defaultProvider, settings.defaultModel = provider, model
	}
	if !failed {
		ai.settings = settings
	}
}

// aiVaultUsable reports whether the vault can be read and written right now: it
// exists, and either has no master password or has been unlocked.
func (state *State) aiVaultUsable() bool {
	opened := state.secrets.vault
	return opened != nil && (!opened.NeedsConversion())
}

// aiAdoptKeys moves API keys that older versions left in plaintext in the AI
// database into the vault. A key that cannot be moved now (no vault, or one that
// is locked) keeps working from memory and is tried again on the next load. The
// keys already in the vault are not read here: opening the settings must not
// make the system ask for access to its secret store. aiKeyedProvider reads a
// key when a request needs it.
func (state *State) aiAdoptKeys() {
	ai := &state.ai
	legacy, err := ai.store.LegacyKeys()
	if err != nil {
		legacy = nil
	}
	usable := state.aiVaultUsable()
	for index := range ai.providers {
		provider := &ai.providers[index]
		if key, ok := legacy[provider.ID]; ok {
			provider.APIKey, provider.HasKey = key, true
			if usable && state.secrets.vault.Set(aiKeySecretName(provider.ID), key) == nil {
				if ai.store.SaveProvider(*provider) == nil {
					_ = ai.store.ClearLegacyKey(provider.ID)
				}
			}
		}
	}
}

// aiKeyedProvider returns provider with its API key in memory. When the key is
// in a vault that is locked it asks for the master password, runs retry once it
// has been given, and reports false: nothing may be sent without the key.
func (state *State) aiKeyedProvider(provider store.Provider, retry func()) (store.Provider, bool) {
	if store.IsCLIKind(provider.Kind) {
		provider.APIKey, provider.HasKey = "", false
		return provider, true
	}
	if provider.APIKey != "" || !provider.HasKey {
		return provider, true
	}
	opened := state.secrets.vault
	if opened == nil {
		return provider, true
	}
	if opened.NeedsConversion() {
		state.openVaultUnlock(nil, func() {
			state.reloadAI()
			retry()
		})
		return provider, false
	}
	if key, found, err := opened.Get(aiKeySecretName(provider.ID)); err == nil && found {
		provider.APIKey = key
	}
	return provider, true
}

// aiModel is the provider and model requests go to, or why there is none.
func (state *State) aiModel() (store.Provider, string, error) {
	ai := &state.ai
	if _, err := state.aiStore(); err != nil {
		return store.Provider{}, "", err
	}
	providerID, model, ok, err := ai.store.DefaultModel()
	if err != nil {
		return store.Provider{}, "", err
	}
	if ok {
		for _, provider := range ai.providers {
			if provider.ID == providerID && provider.Enabled {
				return provider, model, nil
			}
		}
	}
	return store.Provider{}, "", errNoAIModel
}

var errNoAIModel = errors.New("no model is set up")

func (state *State) aiProvider(provider store.Provider) llm.Provider {
	if state.options.AIProvider != nil {
		return state.options.AIProvider(provider)
	}
	if store.IsCLIKind(provider.Kind) || !store.ValidProviderKind(provider.Kind) {
		return cli.New(provider.Kind, provider.CLIPath)
	}
	config := llm.Config{BaseURL: provider.BaseURL, ModelsURL: provider.ModelsURL, APIKey: provider.APIKey, AuthStyle: provider.AuthStyle}
	if provider.Kind == store.KindAnthropic {
		return llm.NewAnthropic(config)
	}
	if provider.APIFormat == store.APIFormatResponses {
		return llm.NewOpenAIResponses(config)
	}
	return llm.NewOpenAI(config)
}

// aiGate is the door the assistant's tool calls go through.
func (state *State) aiGate(mode gate.Mode) gate.Gate {
	return gate.Gate{
		Backend:  state.ai.backend,
		Policy:   risk.Policy{Enabled: true, AutoUpTo: state.aiAutoUpTo()},
		Mode:     mode,
		Approver: state.aiApprover,
	}
}

// aiAutoUpTo is the highest risk this conversation runs without asking: the
// scope picked from the chip, or the setting when none was picked.
func (state *State) aiAutoUpTo() risk.Level {
	if picked := state.ai.autoUpTo; picked != nil {
		return *picked
	}
	return state.ai.settings.autoUpTo
}

// aiSetAutoUpTo picks the scope of this conversation. Picking the setting's own
// scope clears the pick instead of pinning a duplicate, so the chip goes back to
// following the setting. A scope that is fixed for a running request refuses the
// pick here, so no caller can change it behind the request's back.
func (state *State) aiSetAutoUpTo(level risk.Level) {
	if state.aiScopeLocked() {
		return
	}
	if level == state.ai.settings.autoUpTo {
		state.ai.autoUpTo = nil
		return
	}
	picked := level
	state.ai.autoUpTo = &picked
}

// aiScopeLocked reports whether this conversation's scope must stay as it is: a
// request in flight was started under it, and an approval waiting for an answer
// belongs to one.
func (state *State) aiScopeLocked() bool {
	return state.ai.run != nil || state.ai.pendingRun != nil || len(state.ai.approvals) > 0
}

// aiApprover is called from the agent's goroutine. It shows the question in the
// panel and waits for the user.
func (state *State) aiApprover(ctx context.Context, request gate.Request) (bool, string, error) {
	approval := &aiApproval{request: request, reply: make(chan aiAnswer, 1)}
	if !state.aiPost(func(s *State) {
		s.ai.approvals = append(s.ai.approvals, approval)
		// Opening the panel through its own path also reconciles with an open
		// monitor: a narrow window shows one panel, not two stacked drawers.
		s.setAIPanelOpen(true)
		s.markToolWaiting()
	}) {
		return false, "", errAIClosed
	}
	select {
	case answer := <-approval.reply:
		return answer.approved, answer.text, nil
	case <-ctx.Done():
		state.aiPost(func(s *State) { s.dropApproval(approval) })
		return false, "", ctx.Err()
	}
}

func (state *State) markToolWaiting() {
	for index := len(state.ai.items) - 1; index >= 0; index-- {
		if state.ai.items[index].kind == aiTool && state.ai.items[index].status == aiToolRunning {
			state.ai.items[index].status = aiToolWaiting
			return
		}
	}
}

func (state *State) dropApproval(approval *aiApproval) {
	ai := &state.ai
	for index, candidate := range ai.approvals {
		if candidate == approval {
			ai.approvals = slices.Delete(ai.approvals, index, index+1)
			return
		}
	}
}

// aiAnswerApproval gives the user's answer to the first waiting question.
func (state *State) aiAnswerApproval(approved bool) {
	ai := &state.ai
	if len(ai.approvals) == 0 {
		return
	}
	approval := ai.approvals[0]
	typed := ai.confirm.String()
	if approved && approval.request.NeedsText && strings.TrimSpace(typed) != approval.request.Confirm {
		return // the word is not typed yet
	}
	if approval.escalation {
		ai.approvals = slices.Delete(ai.approvals, 0, 1)
		ai.confirm = text.Editor{}
		if approved {
			state.aiApproveEscalation()
		} else {
			state.aiAnswerAfterDecline()
		}
		return
	}
	ai.approvals = ai.approvals[1:]
	ai.confirm = text.Editor{}
	for index := range ai.items {
		if ai.items[index].kind == aiTool && ai.items[index].status == aiToolWaiting {
			ai.items[index].status = aiToolRunning
			break
		}
	}
	approval.reply <- aiAnswer{approved: approved, text: typed}
}

// aiApproveEscalation switches to Agent and retries the user message that is
// already in the history. The Ask run persisted that user message; the Agent
// run therefore continues from it instead of adding a second bubble or a
// dangling escalation tool call.
func (state *State) aiApproveEscalation() {
	ai := &state.ai
	question := ""
	for index := len(ai.history) - 1; index >= 0; index-- {
		if ai.history[index].Role == llm.RoleUser {
			question = strings.TrimSpace(stripContext(ai.history[index].Text))
			break
		}
	}
	if question == "" {
		ai.items = append(ai.items, aiItem{kind: aiNotice, text: state.messages.AIPanel.EscalationMissing})
		state.aiFinishPendingRun(aiRunCompleted)
		return
	}
	state.setAIMode(agent.ModeAgent)
	if ai.pendingRun != nil {
		ai.pendingRun.phase = aiRunWaitingModel
		ai.pendingRun.tool = ""
	}
	ai.retry = true
	ai.answerOnly = false
	ai.input = text.Editor{}
	ai.input.Insert(question)
	if !state.aiSend() {
		state.aiFinishPendingRun(aiRunCompleted)
	}
}

// aiAnswerAfterDecline retries the same Ask question with no tools, so declining
// a switch does not leave the reasoning as the last thing the user sees.
func (state *State) aiAnswerAfterDecline() {
	ai := &state.ai
	question := ""
	for index := len(ai.history) - 1; index >= 0; index-- {
		if ai.history[index].Role == llm.RoleUser {
			question = strings.TrimSpace(stripContext(ai.history[index].Text))
			break
		}
	}
	if question == "" {
		ai.items = append(ai.items, aiItem{kind: aiNotice, text: state.messages.AIPanel.EscalationMissing})
		state.aiFinishPendingRun(aiRunCompleted)
		return
	}
	if ai.pendingRun != nil {
		ai.pendingRun.phase = aiRunWaitingModel
		ai.pendingRun.tool = ""
	}
	ai.retry = true
	ai.answerOnly = true
	ai.input = text.Editor{}
	ai.input.Insert(question)
	if !state.aiSend() {
		state.aiFinishPendingRun(aiRunCompleted)
	}
}

// aiContext attaches the locked default session's recent text in Ask mode.
// A different visible tab does not change the source of this context.
func (state *State) aiContext() string {
	lines := state.ai.settings.contextLines
	if lines <= 0 {
		return ""
	}
	session, err := state.aiTargetSession()
	if err != nil || session.term == nil {
		return ""
	}
	text := lastLines(session.term.screen.Text(), lines)
	if text == "" {
		return ""
	}
	if state.ai.settings.redact {
		text = agent.Redact(text)
	}
	return fmt.Sprintf("\n\n[Terminal of %s (%s), last lines:]\n%s", session.Title, session.Address, text)
}

func (state *State) aiSessionSummary(references ...gate.SessionInfo) string {
	var b strings.Builder
	target := state.aiTarget()
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.term == nil || sessionHandle(session.ID) != target.ID {
			continue
		}
		fmt.Fprintf(&b, "session %s: %s (%s)", sessionHandle(session.ID), session.Title, session.Address)
		fmt.Fprintf(&b, " [locked default; ready=%t]", aiSessionInfo(session).Ready)
		b.WriteByte('\n')
	}
	for _, reference := range references {
		if reference.ID == target.ID {
			continue
		}
		if slices.ContainsFunc(state.ai.extras, func(have gate.SessionInfo) bool { return have.ID == reference.ID }) {
			fmt.Fprintf(&b, "session %s: %s (%s) [selected by the user alongside the default, like an @%s reference; ready=%t]\n",
				reference.ID, reference.Title, reference.Address, mentionSlug(reference.Title), reference.Ready)
			continue
		}
		fmt.Fprintf(&b, "session %s: %s (%s) [explicit @%s reference; ready=%t]\n",
			reference.ID, reference.Title, reference.Address, mentionSlug(reference.Title), reference.Ready)
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiNoSessionNote tells the model there is no terminal while none is open; the
// prompt above it speaks of the user's visible terminal.
func (state *State) aiNoSessionNote(references ...gate.SessionInfo) string {
	if session, err := state.aiTargetSession(); err == nil && aiSessionInfo(session).Ready {
		return ""
	}
	if len(references) > 0 {
		return "\n\nNo ready default connection is selected. Selected connections and explicit @references listed above may still be used by their exact session IDs when the user asks for operations on them. Never fall back to the active tab."
	}
	return "\n\nNo target connection is selected. No session tools are available. Answer from general knowledge or explicitly attached context. Ask the user to select a target connection for operations."
}

// setAIMode changes Ask/Agent and remembers the choice in the AI database.
// The mode controls whether tools are offered; the risk gate still decides what
// may run.
func (state *State) setAIMode(mode agent.Mode) {
	state.dropEscalationApprovals()
	state.ai.panel.autoMenu = false
	if state.ai.mode == mode {
		return
	}
	state.ai.mode = mode
	name := "ask"
	if mode == agent.ModeAgent {
		name = "agent"
	}
	state.aiPut("ai.mode", name)
}

// dropEscalationApprovals removes Ask-mode switch cards. A manual mode change
// or a new conversation makes them stale; normal gate approvals are left alone.
func (state *State) dropEscalationApprovals() {
	kept := state.ai.approvals[:0]
	removed := false
	for _, approval := range state.ai.approvals {
		if !approval.escalation {
			kept = append(kept, approval)
		} else {
			removed = true
		}
	}
	clear(state.ai.approvals[len(kept):])
	state.ai.approvals = kept
	if removed {
		state.aiFinishPendingRun(aiRunCompleted)
	}
}

// aiSend starts a request with the text in the input box.
func (state *State) aiSend() bool {
	ai := &state.ai
	if ai.run != nil {
		return false
	}
	question := strings.TrimSpace(ai.input.String())
	if (question == "" && len(ai.attachments) == 0) || ai.attaching > 0 && !ai.retry {
		return false
	}
	if ai.mode != agent.ModeAgent && slices.ContainsFunc(ai.attachments, func(attachment aiAttachment) bool { return attachment.upload }) {
		ai.notice = state.messages.AIPanel.UploadNeedsAgent
		return false
	}
	provider, model, err := state.aiModel()
	if err != nil {
		ai.notice = err.Error()
		return false
	}
	target := state.aiTarget()
	extras := state.aiExtraTargets()
	references := state.aiRequestReferences(question)
	backend, err := state.aiTargetBackend(target, references...)
	if err != nil {
		ai.notice = state.messages.AIPanel.TargetUnavailable
		return false
	}
	ai.target, ai.extras, ai.targetSet = target, extras, true
	provider, ready := state.aiKeyedProvider(provider, func() { state.aiSend() })
	if !ready {
		return true // keep both the question and target while the vault is unlocked
	}
	retry := ai.retry
	answerOnly := ai.answerOnly
	var attachments []aiAttachment // a retry continues a question that already has its own
	if !retry {
		attachments = ai.attachments
	}
	attachedText, images, refs := aiAttachmentContext(attachments, ai.settings.redact)
	targetKey := ""
	if target.ID != "" {
		if ai.targetNamespace == "" {
			ai.targetNamespace = rand.Text()
		}
		targetKey = ai.targetNamespace + ":" + target.ID
		for _, extra := range extras {
			targetKey += "," + extra.ID
		}
	}
	sent := question + attachedText
	var attached uint32 // the session whose text already went with the question
	if !retry && ai.mode == agent.ModeAsk {
		if context := state.aiContext(); context != "" {
			sent += context
			if session, err := state.aiTargetSession(); err == nil {
				attached = session.ID
			}
		}
	}
	if !retry {
		sent += state.aiMentionContext(question, attached)
	}
	if !retry && ai.settings.keepHistory && ai.store != nil && len(sent) > store.MaxTextBytes {
		ai.notice = fmt.Sprintf(state.messages.AIPanel.MessageTooLargeFmt, formatBytes(store.MaxTextBytes))
		return false
	}
	ai.notice = ""
	ai.needLogin = ""
	ai.retry = false
	ai.answerOnly = false
	ai.input = text.Editor{}
	if !retry {
		ai.attachments = nil
		state.dropEscalationApprovals()
		state.aiAppendQuestion(question, targetKey, state.aiConversationTargetLabel(target, extras...))
		ai.items[len(ai.items)-1].attachments = refs
	}
	ai.scroll.ScrollToEnd()

	config := agent.Config{
		Provider:          state.aiProvider(provider),
		Model:             model,
		System:            agent.DefaultSystem(state.aiSessionSummary(references...)) + state.aiNoSessionNote(references...),
		Mode:              ai.mode,
		UserInHistory:     retry,
		UserImages:        images,
		NoTools:           answerOnly,
		Gate:              state.aiGate(gate.ModeAsk),
		MaxSteps:          ai.settings.maxSteps,
		StepTimeout:       15 * time.Minute, // includes the time a person takes to answer
		RedactToolResults: ai.settings.redact,
		ReasoningEffort:   state.aiEffectiveReasoningEffort(),
	}
	config.Gate.Target = &target
	if ai.mode == agent.ModeAgent {
		config.Tools = tools.Targeted()
		for index, attachment := range attachments {
			if attachment.upload {
				if backend.uploads == nil {
					backend.uploads = make(map[string]aiUploadFile)
				}
				backend.uploads[strconv.Itoa(index+1)] = aiUploadFile{path: attachment.path, name: attachment.file.Name, size: attachment.size, modified: attachment.modified, info: attachment.info}
			}
		}
		if len(backend.uploads) > 0 {
			config.Tools = append(config.Tools, tools.SelectedUpload())
		}
	}
	config.Gate.References = references
	config.Gate.Backend = backend
	config.System += "\n\nConnection routing for this request: the selected primary connection is the locked default. Omitted session arguments ALWAYS resolve to that default, never to the active tab or a referenced session. Only that default, the other connections the user selected and the explicit @references listed above are available to tools for this question. A reference by itself attaches context, not an instruction to act: when the user only asks to compare or analyze the attached text, answer from it. When the user explicitly asks to inspect, run a command, or modify files on an @referenced connection, use its exact session ID in the tool arguments; do NOT ask them to switch the primary selector. Cross-connection execution and writes require approval showing the actual target. For a request covering several explicitly named connections, use the specified IDs; never include an unmentioned connection from history or list_sessions. Changing tabs does not change any of these bindings."
	ctx, cancel := context.WithCancel(context.Background())
	stats := aiRunStats{startedAt: state.now()}
	var usageBase llm.Usage
	if retry && ai.pendingRun != nil {
		stats = *ai.pendingRun
		stats.duration = 0
		stats.phase = aiRunWaitingModel
		stats.tool = ""
		usageBase = stats.usage
		ai.pendingRun = nil
	}
	run := &aiRun{
		stats: stats, usageBase: usageBase, cancel: cancel, open: -1, providerID: provider.ID,
		target: target, extras: extras, targetKey: targetKey, attachments: refs,
	}
	ai.run = run
	backend.uploadRun = run
	ai.lastRun = nil
	ai.panel.autoMenu = false // the scope is fixed while the request runs
	if backend.defaultDone != nil && len(backend.bindings) == 1 {
		go state.aiWatchTarget(ctx, run, backend.defaultDone)
	}
	history := append([]llm.Message(nil), ai.history...)
	if aiHasAttachments(history, sent) {
		config.System += attachmentNote
	}
	go func() {
		defer backend.closeAll()
		messages, err := agent.Run(ctx, config, history, sent, func(event agent.Event) {
			state.aiPost(func(s *State) { s.aiEvent(run, event) })
		})
		state.aiPost(func(s *State) { s.aiFinished(run, messages, err) })
	}()
	return true
}

// aiStop cancels the request in flight.
func (state *State) aiStop() {
	if run := state.ai.run; run != nil {
		run.stats.phase = aiRunStopping
		run.cancel()
	}
}

func (state *State) aiEvent(run *aiRun, event agent.Event) {
	if event.Kind == agent.EventError {
		if state.ai.run == run && event.Text == "cli: login_required" {
			state.ai.needLogin = run.providerID
		}
		event.Text = state.aiCLIError(event.Text)
	}
	ai := &state.ai
	if ai.run != run {
		return
	}
	if event.Usage != (llm.Usage{}) {
		run.stats.usage = run.usageBase.Add(event.Usage)
	}
	run.stats.usageIncomplete = run.stats.usageIncomplete || event.UsageIncomplete
	appendText := func(kind aiItemKind, delta string) {
		if run.open >= 0 && run.open < len(ai.items) && ai.items[run.open].kind == kind {
			item := &ai.items[run.open]
			if run.text == nil || run.text.Len() != len(item.text) {
				// The item was not started here (or the run reused an item):
				// seed the builder with what it already shows.
				run.text = &strings.Builder{}
				run.text.WriteString(item.text)
			}
			run.text.WriteString(delta)
			item.text = run.text.String()
			return
		}
		// A new item gets a new builder: resetting one would overwrite the
		// buffer the previous item's text still points into.
		run.text = &strings.Builder{}
		run.text.WriteString(delta)
		ai.items = append(ai.items, aiItem{kind: kind, text: run.text.String()})
		run.open = len(ai.items) - 1
	}
	switch event.Kind {
	case agent.EventText:
		if run.stats.phase != aiRunStopping {
			run.stats.phase = aiRunAnswering
		}
		appendText(aiAssistant, event.Text)
	case agent.EventReasoning:
		if run.stats.phase != aiRunStopping {
			run.stats.phase = aiRunAnalyzing
		}
		appendText(aiReasoning, event.Text)
	case agent.EventToolStart:
		if run.stats.phase != aiRunStopping {
			run.stats.phase, run.stats.tool = aiRunUsingTool, event.Call.Name
		}
		run.open = -1
		item := aiItem{kind: aiTool, call: event.Call, status: aiToolRunning}
		if event.Call.Name == "terminal_exec" || event.Call.Name == "terminal_exec_many" {
			var args struct {
				Command string `json:"command"`
			}
			if json.Unmarshal([]byte(event.Call.Arguments), &args) == nil {
				item.risk = risk.Classify(args.Command).Level
			}
		}
		ai.items = append(ai.items, item)
	case agent.EventToolDone:
		if run.stats.phase != aiRunStopping {
			run.stats.phase = aiRunWaitingModel
		}
		run.open = -1
		for index := len(ai.items) - 1; index >= 0; index-- {
			item := &ai.items[index]
			if item.kind != aiTool || item.call.ID != event.Call.ID {
				continue
			}
			item.result = event.Result.Text
			switch {
			case event.Result.Denied:
				item.status = aiToolDenied
			case event.Result.IsError:
				item.status = aiToolFailed
			default:
				item.status = aiToolDone
			}
			break
		}
	case agent.EventAgentModeRequest:
		run.open = -1
		args, _ := tools.DecodeRequestAgentMode([]byte(event.Call.Arguments))
		reason := strings.TrimSpace(args.Reason)
		summary := strings.TrimSpace(args.Command)
		if summary == "" {
			summary = state.messages.AIPanel.EscalationSummary
		}
		request := gate.Request{Tool: tools.NameRequestAgentMode, Kind: tools.Read, Summary: summary}
		request.Session, request.SessionLabel = run.target.ID, state.aiTargetLabel(run.target)
		if reason != "" {
			request.Assessment.Reasons = []string{reason}
		}
		for index := range ai.approvals {
			if ai.approvals[index].escalation {
				ai.approvals[index].request = request
				return
			}
		}
		ai.approvals = append(ai.approvals, &aiApproval{request: request, escalation: true})
		ai.scroll.ScrollToEnd()
	case agent.EventDone:
		if llm.StopsAtLimit(event.StopReason) {
			// Half an answer must not look like the whole of it.
			ai.items = append(ai.items, aiItem{kind: aiNotice, text: state.messages.AIPanel.AnswerTruncated})
		}
	case agent.EventError:
		ai.items = append(ai.items, aiItem{kind: aiNotice, text: errorText(event.Text)})
	}
}

// aiUsageText is the token accounting shown under the last answer: the
// finished run, and behind it the running total of the conversation while the
// window is open.
func (state *State) aiUsageText() string {
	m := state.messages.AIPanel
	last := fmt.Sprintf(m.UsageFmt, groupDigits(state.ai.lastUsage.InputTokens), groupDigits(state.ai.lastUsage.OutputTokens))
	if state.ai.lastUsage.CacheReadTokens > 0 {
		last += fmt.Sprintf(m.UsageCachedFmt, groupDigits(state.ai.lastUsage.CacheReadTokens))
	}
	if state.ai.totalUsage.InputTokens > state.ai.lastUsage.InputTokens ||
		state.ai.totalUsage.OutputTokens > state.ai.lastUsage.OutputTokens {
		last += fmt.Sprintf(m.UsageTotalFmt, groupDigits(state.ai.totalUsage.InputTokens), groupDigits(state.ai.totalUsage.OutputTokens))
	}
	return last
}

// groupDigits writes n with a comma every three digits.
func groupDigits(n int) string {
	digits := strconv.Itoa(n)
	if n < 0 {
		return digits
	}
	var out []byte
	for index, digit := range []byte(digits) {
		if index > 0 && (len(digits)-index)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digit)
	}
	return string(out)
}

func errorText(s string) string { return strings.TrimSpace(s) }

// aiFinished ends a request: the messages it made join the history and, if the
// user keeps history, the database.
func (state *State) aiFinished(run *aiRun, messages []llm.Message, err error) {
	ai := &state.ai
	if ai.run != run {
		return
	}
	run.stats.duration = max(state.now()-run.stats.startedAt, 0)
	run.stats.usageIncomplete = run.stats.usageIncomplete || err != nil
	pendingApproval := false
	switch {
	case errors.Is(err, context.Canceled):
		run.stats.phase = aiRunStopped
	case err != nil:
		run.stats.phase = aiRunFailed
	default:
		run.stats.phase = aiRunCompleted
		for _, approval := range ai.approvals {
			if approval.escalation {
				run.stats.phase = aiRunNeedsApproval
				pendingApproval = true
				break
			}
		}
	}
	stats := run.stats
	if pendingApproval {
		ai.pendingRun = &stats
		ai.lastRun = ai.pendingRun
	} else {
		state.aiRecordFinishedRun(&stats)
	}
	ai.run = nil
	run.cancel()
	if err != nil {
		for index := range ai.items {
			item := &ai.items[index]
			if item.kind == aiTool && (item.status == aiToolRunning || item.status == aiToolWaiting) {
				item.status = aiToolFailed
				if item.result == "" && errors.Is(err, context.Canceled) {
					item.result = state.messages.AIPanel.RunStopped
				}
			}
		}
	}
	kept := ai.approvals[:0]
	for _, approval := range ai.approvals {
		if approval.escalation {
			kept = append(kept, approval)
			continue
		}
		approval.reply <- aiAnswer{}
	}
	clear(ai.approvals[len(kept):])
	ai.approvals = kept
	if err != nil && !errors.Is(err, context.Canceled) {
		if n := len(ai.items); n == 0 || ai.items[n-1].kind != aiNotice {
			ai.items = append(ai.items, aiItem{kind: aiNotice, text: state.aiCLIError(err.Error())})
		}
	}
	ai.history = append(ai.history, messages...)
	state.aiPersist(messages, run.target, run.extras, run.targetKey, run.attachments)
}

func (state *State) aiPersist(messages []llm.Message, target gate.SessionInfo, extras []gate.SessionInfo, key string, attachments []aiAttachmentRef) {
	ai := &state.ai
	if !ai.settings.keepHistory || ai.store == nil || len(messages) == 0 {
		return
	}
	if ai.conv == 0 {
		title := strings.TrimSpace(stripContext(messages[0].Text))
		if title == "" && len(attachments) > 0 {
			title = attachments[0].Name
		}
		if r := []rune(title); len(r) > 40 {
			title = string(r[:40])
		}
		label := target.Title
		for _, extra := range extras {
			label += ", " + extra.Title
		}
		conversation, err := ai.store.CreateConversation(strings.TrimSpace(title), label)
		if err != nil {
			ai.notice = fmt.Sprintf(state.messages.AIPanel.HistorySaveFailedFmt, err)
			return
		}
		ai.conv = conversation.ID
	}
	for _, message := range messages {
		stored := toStored(message)
		if message.Role == llm.RoleUser || len(message.ToolCalls) > 0 {
			var metadata storedTools
			_ = json.Unmarshal(stored.ToolJSON, &metadata)
			metadata.TargetLabel = state.aiConversationTargetLabel(target, extras...)
			metadata.TargetKey = key
			if message.Role == llm.RoleUser {
				metadata.Attachments = attachments
			}
			stored.ToolJSON, _ = json.Marshal(metadata)
		}
		if _, err := ai.store.AppendMessage(ai.conv, stored); err != nil {
			ai.notice = fmt.Sprintf(state.messages.AIPanel.HistorySaveFailedFmt, err)
			return
		}
	}
}

// storedTools is how tool calls and results travel through a StoredMessage.
type storedTools struct {
	TargetKey   string `json:"target_key,omitempty"`
	TargetLabel string `json:"target_label,omitempty"`
	// Attachments are the names of what a question was sent with. The files are
	// not kept: a saved conversation shows them but the model no longer sees them.
	Attachments []aiAttachmentRef `json:"attachments,omitempty"`
	Calls       []llm.ToolCall    `json:"calls,omitempty"`
	CallID      string            `json:"call_id,omitempty"`
}

func toStored(message llm.Message) store.StoredMessage {
	stored := store.StoredMessage{Role: string(message.Role), Text: message.Text}
	if len(message.ToolCalls) > 0 || message.ToolCallID != "" {
		stored.ToolJSON, _ = json.Marshal(storedTools{Calls: message.ToolCalls, CallID: message.ToolCallID})
	}
	return stored
}

func fromStored(stored store.StoredMessage) llm.Message {
	message := llm.Message{Role: llm.Role(stored.Role), Text: stored.Text}
	if len(stored.ToolJSON) > 0 {
		var tools storedTools
		if json.Unmarshal(stored.ToolJSON, &tools) == nil {
			message.ToolCalls, message.ToolCallID = tools.Calls, tools.CallID
		}
	}
	return message
}

// aiNewConversation starts over; the old one stays in the database.
func (state *State) aiNewConversation() {
	ai := &state.ai
	if ai.run != nil {
		return
	}
	ai.target, ai.extras, ai.targetSet = gate.SessionInfo{}, nil, false
	ai.panel.targetOpen = false
	ai.panel.autoMenu = false
	ai.items, ai.history, ai.conv, ai.notice = nil, nil, 0, ""
	state.aiClearAttachments()
	ai.approvals = nil
	ai.autoUpTo = nil // a new conversation starts from the setting
	ai.retry = false
	ai.answerOnly = false
	ai.lastUsage, ai.totalUsage = llm.Usage{}, llm.Usage{}
	ai.lastRun = nil
	ai.pendingRun = nil
	ai.scroll.ScrollToEnd()
	ai.panel.clearAITextSelection()
}

// aiOpenConversation shows a saved conversation and continues from it.
func (state *State) aiOpenConversation(id int64) {
	ai := &state.ai
	if ai.run != nil || ai.store == nil {
		return
	}
	stored, err := ai.store.Messages(id)
	if err != nil {
		ai.notice = err.Error()
		return
	}
	ai.items, ai.history, ai.conv = nil, nil, id
	state.aiClearAttachments()
	// Persisted terminal IDs are not identities across application restarts.
	// History restores neither a live default nor reference authorization. A new
	// request must select its default or explicitly name its reference sessions.
	ai.target, ai.extras, ai.targetSet = gate.SessionInfo{}, nil, true
	ai.panel.targetOpen = false
	ai.panel.autoMenu = false
	ai.autoUpTo = nil // another conversation's scope does not follow this one
	ai.notice = ""
	ai.retry, ai.answerOnly = false, false
	state.dropEscalationApprovals()
	ai.lastUsage, ai.totalUsage = llm.Usage{}, llm.Usage{}
	ai.lastRun = nil
	ai.pendingRun = nil
	ai.scroll.ScrollToEnd()
	ai.panel.clearAITextSelection()
	for _, row := range stored {
		var metadata storedTools
		_ = json.Unmarshal(row.ToolJSON, &metadata)
		message := fromStored(row)
		ai.history = append(ai.history, message)
		switch message.Role {
		case llm.RoleUser:
			state.aiAppendQuestion(stripContext(message.Text), metadata.TargetKey, metadata.TargetLabel)
			ai.items[len(ai.items)-1].attachments = metadata.Attachments
		case llm.RoleAssistant:
			if message.Text != "" {
				ai.items = append(ai.items, aiItem{kind: aiAssistant, text: message.Text})
			}
			for _, call := range message.ToolCalls {
				ai.items = append(ai.items, aiItem{kind: aiTool, call: call, status: aiToolDone})
			}
		case llm.RoleTool:
			for index := len(ai.items) - 1; index >= 0; index-- {
				if ai.items[index].kind == aiTool && ai.items[index].call.ID == message.ToolCallID {
					ai.items[index].result = message.Text
					break
				}
			}
		}
	}
}

// stripContext removes what was appended to a question: the files attached to it
// and the terminal excerpt Ask mode added. They are the first thing after it, so
// whichever heading comes first ends the question.
func stripContext(text string) string {
	end := len(text)
	for _, marker := range [...]string{"\n\n[Terminal of ", attachedMarker} {
		if index := strings.Index(text, marker); index >= 0 {
			end = min(end, index)
		}
	}
	return text[:end]
}

// aiCurrentModel 返回当前选中的模型（从已常驻内存的模型列表与设置中获取，避免在绘制循环中触发数据库查询）。
func (state *State) aiCurrentModel() *store.Model {
	ai := &state.ai
	if ai.settings.defaultModel == "" || ai.settings.defaultProvider == "" {
		return nil
	}
	for i := range ai.models {
		if ai.models[i].ProviderID == ai.settings.defaultProvider && ai.models[i].Name == ai.settings.defaultModel {
			return &ai.models[i]
		}
	}
	return nil
}

// aiCurrentModelReasoning 返回当前模型及其支持的思考档位列表，以及是否支持思考控制。
func (state *State) aiCurrentModelReasoning() (*store.Model, []string, bool) {
	m := state.aiCurrentModel()
	if m == nil {
		return nil, nil, false
	}
	if state.ai.reasoningCachedModel == m.Name &&
		state.ai.reasoningCachedConfig == m.Reasoning &&
		state.ai.reasoningCachedDisabled == m.ReasoningDisabled {
		return m, state.ai.reasoningCachedLevels, state.ai.reasoningCachedOK
	}

	state.ai.reasoningCachedModel = m.Name
	state.ai.reasoningCachedConfig = m.Reasoning
	state.ai.reasoningCachedDisabled = m.ReasoningDisabled

	if m.ReasoningDisabled {
		state.ai.reasoningCachedLevels, state.ai.reasoningCachedOK = nil, false
		return m, nil, false
	}
	if m.Reasoning != nil && len(m.Reasoning.Levels) > 0 {
		state.ai.reasoningCachedLevels, state.ai.reasoningCachedOK = m.Reasoning.Levels, true
		return m, m.Reasoning.Levels, true
	}
	if inferred := reasoning.InferReasoning(m.Name); inferred != nil && len(inferred.Levels) > 0 {
		state.ai.reasoningCachedLevels, state.ai.reasoningCachedOK = inferred.Levels, true
		return m, inferred.Levels, true
	}
	state.ai.reasoningCachedLevels, state.ai.reasoningCachedOK = nil, false
	return m, nil, false
}

// aiModelReasoningInfo 返回任意模型的思考水平状态：档位列表、默认档位、是否支持思考、是否为用户覆写、是否被用户强制禁用。
func (state *State) aiModelReasoningInfo(m *store.Model) (levels []string, def string, isReasoning bool, isOverride bool, isDisabled bool) {
	if m == nil {
		return nil, "", false, false, false
	}
	if m.ReasoningDisabled {
		return nil, "", false, true, true
	}
	if m.Reasoning != nil && len(m.Reasoning.Levels) > 0 {
		return m.Reasoning.Levels, m.Reasoning.Default, true, true, false
	}
	if inferred := reasoning.InferReasoning(m.Name); inferred != nil && len(inferred.Levels) > 0 {
		return inferred.Levels, inferred.Default, true, false, false
	}
	return nil, "", false, false, false
}

// aiEffectiveReasoningEffort 计算当前请求应该发送给 LLM 的具体思考档位。
func (state *State) aiEffectiveReasoningEffort() string {
	m, levels, ok := state.aiCurrentModelReasoning()
	if !ok || len(levels) == 0 {
		return ""
	}
	override := state.ai.panel.reasoningEffort
	if override != "" && slices.Contains(levels, override) {
		return override
	}
	if m.Reasoning != nil {
		if eff, ok := m.Reasoning.EffectiveLevel(override); ok {
			return eff
		}
	} else if inferred := reasoning.InferReasoning(m.Name); inferred != nil {
		return inferred.Default
	}
	return ""
}

// aiReasoningEffortLabel 返回指定档位的国际化展示名称。
func (state *State) aiReasoningEffortLabel(effort string) string {
	m := state.messages.AIPanel
	switch effort {
	case reasoning.LevelNone:
		return m.ReasoningNone
	case reasoning.LevelMinimal:
		return m.ReasoningMinimal
	case reasoning.LevelLow:
		return m.ReasoningLow
	case reasoning.LevelMedium:
		return m.ReasoningMedium
	case reasoning.LevelHigh:
		return m.ReasoningHigh
	case reasoning.LevelXHigh:
		return m.ReasoningXHigh
	case reasoning.LevelMax:
		return m.ReasoningMax
	case reasoning.LevelDefault:
		return m.ReasoningDefault
	default:
		return m.ReasoningAuto
	}
}
