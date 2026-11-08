package i18n

// AISettingsMessages are the texts of the assistant's settings section.
type AISettingsMessages struct {
	Title       string
	Description string
	// StoreErrorFmt takes the reason the assistant's database could not be used.
	StoreErrorFmt string

	// Providers.
	ProvidersTitle     string
	ProvidersEmpty     string
	ProviderAdd        string
	ProviderImport     string
	ProviderEdit       string
	ProviderDelete     string
	ProviderEnabled    string // accessible name of a provider's switch
	ProviderKeySaved   string
	ProviderNoKey      string
	ProviderOfficial   string // shown for a provider that uses its official endpoint
	ProviderKindOpenAI string
	ProviderKindClaude string
	ProviderAuthAPIKey string
	ProviderAuthBearer string
	// ProviderFormatResponses is shown in the provider row of an OpenAI-compatible
	// provider that uses the Responses API. Chat Completions, the default, is not
	// named there.
	ProviderFormatResponses string

	// The provider dialog.
	FormAddTitle        string
	FormEditTitle       string
	FormDescription     string
	FormName            string
	FormNamePH          string
	FormKind            string
	FormAuth            string
	FormAuthAPIKey      string
	FormAuthBearer      string
	FormFormat          string
	FormFormatChat      string
	FormFormatResponses string
	// The route each protocol is sent to, shown under the choice.
	FormFormatChatRoute      string
	FormFormatResponsesRoute string
	FormPreset               string
	FormPresetCustom         string
	FormBaseURL              string
	FormBaseURLOpenAI        string // placeholder: the official default
	FormBaseURLClaude        string
	FormModelsURL            string
	FormModelsURLPH          string
	FormKey                  string
	FormKeyPH                string
	FormKeySaved             string
	FormSave                 string
	FormAdd                  string
	ErrFormName              string
	ErrFormBaseURL           string
	ErrFormModelsURL         string
	ErrFormKey               string
	ErrFormSaveFmt           string
	ErrModelName             string
	ErrModelNotChat          string
	CLIPath                  string
	CLIPathDefault           string
	CLIPathDetected          string
	CLIPathNotFound          string
	CLIFormDescription       string
	CLIDefaultModel          string
	CLIModelsNote            string
	CLIModelsCheck           string

	CLIAuthTitle      string
	CLIAuthLogin      string
	CLIAuthDevice     string
	CLIAuthRecheck    string
	CLIAuthUnknown    string
	CLIAuthSignedOut  string
	CLIAuthConfigured string
	CLIAuthChecking   string
	CLIAuthSigningIn  string
	CLIAuthFailed     string
	CLIAuthCanceled   string
	CLIAuthTimeout    string
	CLIAuthSensitive  string
	CLIErrInstall     string
	CLIErrVersion     string
	CLIErrLogin       string
	CLIErrRunFmt      string

	CLIErrCompatibilityFmt string
	CLIErrCheckUnavailable string
	CLIErrCheckInvalid     string
	CLIErrCapabilityFmt    string
	CLIErrIsolationFmt     string
	CLIVersionUnknown      string

	// Importing from magpie and cc-switch.
	ImportSourcesTitle              string
	ImportSourcesDesc               string
	ImportChooseTitle               string
	ImportChooseFile                string
	ImportChooseFromFile            string
	ImportChooseFileHint            string
	ImportPreview                   string
	ImportSourceMissing             string
	ImportSourceErrorFmt            string
	ImportSourceCountFmt            string
	ImportNoSources                 string
	ImportFileFailedFmt             string
	ImportPreviewTitle              string
	ImportPreviewDesc               string
	ImportSelectedFmt               string
	ImportUnavailableFmt            string
	ImportNoCandidates              string
	ImportBack                      string
	ImportApply                     string
	ImportGatewaySource             string
	ImportGatewayNote               string
	ImportModelsFmt                 string
	ImportReasonDisabled            string
	ImportReasonNoEndpoint          string
	ImportReasonUnsupportedProtocol string
	ImportReasonResponses           string
	ImportReasonOAuth               string
	ImportReasonGemini              string
	ImportReasonProxy               string
	ImportReasonUnsupportedApp      string
	ImportReasonAmbiguous           string
	ImportReasonNoChat              string
	ImportReasonUnsupportedAuth     string
	ImportReasonMalformed           string
	ImportReasonCountFmt            string
	ImportDoneTitle                 string
	ImportSummaryFmt                string
	ImportKeyFailedFmt              string
	ImportFailedFmt                 string
	ImportGatewayImported           string
	ImportClose                     string

	// Deleting a provider.
	DeleteTitle   string
	DeleteBodyFmt string
	DeleteConfirm string

	// Models.
	ModelsTitle      string
	ModelsNoProvider string
	ModelsOfFmt      string
	ModelsDescFmt    string
	ModelsFetch      string
	ModelsFetching   string
	ModelsCancel     string
	ModelsFetchedFmt string
	ModelsFailedFmt  string
	ModelsEmpty      string
	ModelDefault     string
	ModelUse         string
	ModelEnabled     string // accessible name of a model's switch
	ModelAddPH       string
	ModelAdd         string
	ModelPageFmt     string
	ModelPrev        string
	ModelNext        string

	// Probing.
	ModelsProbeAll    string
	ModelsProbeCancel string
	ProbeDialogTitle  string
	ProbeDialogDesc   string
	ProbeStartAction  string
	ModelsProbingFmt  string
	ProbeConcurrency  string
	ProbeTimeout      string
	ModelProbe        string
	ModelProbing      string
	ModelProbeSkipped string
	ModelProbeFailed  string
	ModelTagReasoning string

	// Model Reasoning configuration dialog
	ModelReasoningTitle       string
	ModelReasoningDesc        string
	ModelReasoningMode        string
	ModelReasoningAuto        string
	ModelReasoningAutoDesc    string
	ModelReasoningEnable      string
	ModelReasoningEnableDesc  string
	ModelReasoningDisable     string
	ModelReasoningDisableDesc string
	ModelReasoningPreset      string
	ModelReasoningPresetDesc0 string
	ModelReasoningPresetDesc1 string
	ModelReasoningPresetDesc2 string
	ModelReasoningPreset3     string
	ModelReasoningPreset6     string
	ModelReasoningPreset2     string
	ModelReasoningSave        string
	ModelReasoningTagAuto     string
	ModelReasoningTagManual   string
	ModelReasoningTagOff      string

	// Behaviour.
	BehaviourTitle          string
	AutoRun                 string
	AutoRunDesc             string
	RiskLow                 string
	RiskMedium              string
	RiskHigh                string
	MaxSteps                string
	MaxStepsDesc            string
	ContextLines            string
	ContextLinesDesc        string
	ContextOff              string
	AttachmentTextLimit     string
	AttachmentTextLimitDesc string
	Redact                  string
	RedactDesc              string
	History                 string
	HistoryDesc             string
	ClearHistory            string
	ClearHistoryDesc        string
	ClearHistoryDone        string
	ClearHistoryBusy        string
	ClearTitle              string
	ClearBody               string
	ClearConfirm            string

	// External access.
	MCPEnable          string
	MCPHTTPTitle       string
	MCPHTTPDescription string
	MCPEnableDesc      string
	MCPMode            string
	MCPModeDesc        string
	MCPModeObserve     string
	MCPModeAsk         string
	MCPEndpoint        string
	MCPNotRunning      string
	MCPOff             string
	MCPCommand         string
	MCPCopy            string
	MCPCopied          string
	MCPCopyFailed      string
	MCPToken           string
	MCPTokenDesc       string
	MCPReset           string
	MCPResetDone       string
	MCPConfig          string
	MCPConfigDesc      string
	MCPCopyURL         string
	MCPCopyToken       string
	MCPClaudeHint      string
	MCPCursorHint      string
	MCPCodexHint       string
	MCPCherryHint      string
	MCPCodeBuddyHint   string
	MCPDeepSeekHint    string
	MCPOpenCodeHint    string
	MCPPiHint          string
	MCPQoderHint       string
	MCPTraeHint        string
	MCPVSCodeHint      string
	MCPWindsurfHint    string
	MCPWorkBuddyHint   string
	MCPZCodeHint       string
	MCPClientLabel     string
	MCPPreviewHint     string

	MCPModeAllowAll    string
	MCPAllowAllTitle   string
	MCPAllowAllBody    string
	MCPAllowAllConfirm string
	MCPAllowAllWarning string

	// Agents that work without the window (motty mcp-serve).
	AgentsTitle          string
	AgentsDescription    string
	AgentAllowAllBody    string
	AgentAllowAllWarning string
	AgentMode            string
	AgentModeDesc        string
	AgentModeObserve     string
	AgentModeRun         string
	AgentHosts           string
	AgentHostsNoneDesc   string
	AgentConfig          string
	AgentConfigDesc      string
	AgentActivity        string
	AgentActivityNone    string
	AgentActivityDesc    string
	AgentActivityClear   string
	AgentOutcomeOK       string
	AgentOutcomeFailed   string
	AgentOutcomeRefused  string
}
