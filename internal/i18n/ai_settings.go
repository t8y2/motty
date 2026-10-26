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
	ProviderEdit       string
	ProviderDelete     string
	ProviderEnabled    string // accessible name of a provider's switch
	ProviderKeySaved   string
	ProviderNoKey      string
	ProviderOfficial   string // shown for a provider that uses its official endpoint
	ProviderKindOpenAI string
	ProviderKindClaude string

	// The provider dialog.
	FormAddTitle      string
	FormEditTitle     string
	FormDescription   string
	FormName          string
	FormNamePH        string
	FormKind          string
	FormPreset        string
	FormPresetCustom  string
	FormBaseURL       string
	FormBaseURLOpenAI string // placeholder: the official default
	FormBaseURLClaude string
	FormKey           string
	FormKeyPH         string
	FormKeySaved      string
	FormSave          string
	FormAdd           string
	ErrFormName       string
	ErrFormBaseURL    string
	ErrFormKey        string
	ErrFormSaveFmt    string
	ErrModelName      string

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

	// Behaviour.
	BehaviourTitle   string
	AutoRun          string
	AutoRunDesc      string
	RiskLow          string
	RiskMedium       string
	RiskHigh         string
	MaxSteps         string
	MaxStepsDesc     string
	ContextLines     string
	ContextLinesDesc string
	ContextOff       string
	Redact           string
	RedactDesc       string
	History          string
	HistoryDesc      string
	ClearHistory     string
	ClearHistoryDesc string
	ClearHistoryDone string
	ClearHistoryBusy string
	ClearTitle       string
	ClearBody        string
	ClearConfirm     string

	// External access.
	MCPEnable      string
	MCPEnableDesc  string
	MCPMode        string
	MCPModeDesc    string
	MCPModeObserve string
	MCPModeAsk     string
	MCPEndpoint    string
	MCPNotRunning  string
	MCPOff         string
	MCPCopy        string
	MCPCopied      string
	MCPCopyFailed  string
	MCPToken       string
	MCPTokenDesc   string
	MCPReset       string
	MCPResetDone   string
	MCPConfig      string
	MCPConfigDesc  string
	MCPCopyURL     string
	MCPCopyToken   string
	MCPClaudeHint  string
	MCPCursorHint  string
	MCPCodexHint   string
	MCPPreviewHint string
}

var englishAISettings = AISettingsMessages{
	Title:         "AI assistant",
	Description:   "Models, safety limits and access for the built-in assistant.",
	StoreErrorFmt: "The assistant's database could not be opened: %s",

	ProvidersTitle:     "Providers",
	ProvidersEmpty:     "No provider yet. Add one to use the assistant.",
	ProviderAdd:        "Add provider",
	ProviderEdit:       "Edit",
	ProviderDelete:     "Delete",
	ProviderEnabled:    "Enabled",
	ProviderKeySaved:   "key saved",
	ProviderNoKey:      "no key",
	ProviderOfficial:   "official endpoint",
	ProviderKindOpenAI: "OpenAI-compatible",
	ProviderKindClaude: "Anthropic",

	FormAddTitle:      "Add provider",
	FormEditTitle:     "Edit provider",
	FormDescription:   "The key is stored on this computer and is never shown again.",
	FormName:          "Name",
	FormNamePH:        "My provider",
	FormKind:          "API type",
	FormPreset:        "Provider",
	FormPresetCustom:  "Custom",
	FormBaseURL:       "Base URL",
	FormBaseURLOpenAI: "https://api.openai.com/v1 (default)",
	FormBaseURLClaude: "https://api.anthropic.com (default)",
	FormKey:           "API key",
	FormKeyPH:         "Paste the key",
	FormKeySaved:      "A key is saved. Leave empty to keep it.",
	FormSave:          "Save",
	FormAdd:           "Add",
	ErrFormName:       "Give the provider a name.",
	ErrFormBaseURL:    "The base URL must start with http:// or https://.",
	ErrFormKey:        "The official endpoint needs an API key.",
	ErrFormSaveFmt:    "Could not save: %s",
	ErrModelName:      "A model name has no spaces.",

	DeleteTitle:   "Delete provider?",
	DeleteBodyFmt: "\"%s\", its models and its saved key will be removed.",
	DeleteConfirm: "Delete",

	ModelsTitle:      "Models",
	ModelsNoProvider: "Add a provider to manage its models.",
	ModelsOfFmt:      "Models of %s",
	ModelsDescFmt:    "%d models, %d enabled. The default one answers new questions.",
	ModelsFetch:      "Fetch models",
	ModelsFetching:   "Fetching...",
	ModelsCancel:     "Cancel",
	ModelsFetchedFmt: "%d models found.",
	ModelsFailedFmt:  "Could not fetch models: %s",
	ModelsEmpty:      "No models yet. Fetch them, or type a model name below.",
	ModelDefault:     "Default",
	ModelUse:         "Use",
	ModelEnabled:     "Enabled",
	ModelAddPH:       "Model name",
	ModelAdd:         "Add model",
	ModelPageFmt:     "%d-%d of %d",
	ModelPrev:        "Previous",
	ModelNext:        "Next",

	BehaviourTitle:   "Assistant behaviour",
	AutoRun:          "Run without asking",
	AutoRunDesc:      "Commands up to this risk run on their own. Critical commands always ask first.",
	RiskLow:          "Low",
	RiskMedium:       "Medium",
	RiskHigh:         "High",
	MaxSteps:         "Maximum steps",
	MaxStepsDesc:     "How many tool calls the agent may make for one question.",
	ContextLines:     "Terminal context",
	ContextLinesDesc: "Lines of the visible terminal sent with a question in Ask mode.",
	ContextOff:       "Off",
	Redact:           "Hide secrets",
	RedactDesc:       "Mask passwords, keys and tokens before text is sent to the model.",
	History:          "Keep history",
	HistoryDesc:      "Conversations stay on this computer.",
	ClearHistory:     "Clear history",
	ClearHistoryDesc: "Delete every saved conversation.",
	ClearHistoryDone: "History cleared.",
	ClearHistoryBusy: "Wait for the running request to finish.",
	ClearTitle:       "Clear all history?",
	ClearBody:        "Every saved conversation will be deleted. This cannot be undone.",
	ClearConfirm:     "Clear history",

	MCPEnable:      "Allow external agents (MCP)",
	MCPEnableDesc:  "Lets tools such as Claude Code use your sessions through a local server.",
	MCPMode:        "Access level",
	MCPModeDesc:    "Observe only reads. Ask also runs commands, each one approved here first.",
	MCPModeObserve: "Observe",
	MCPModeAsk:     "Ask",
	MCPEndpoint:    "Endpoint",
	MCPNotRunning:  "Not running",
	MCPOff:         "Off",
	MCPCopy:        "Copy configuration",
	MCPConfig:      "Client configuration",
	MCPConfigDesc:  "Choose a client. Access permissions apply to every client.",
	MCPCopyURL:     "Copy URL",
	MCPCopyToken:   "Copy token",
	MCPClaudeHint:  "Run in your project terminal to register Motty with Claude Code.",
	MCPCursorHint:  "Merge into .cursor/mcp.json in your project, preserving existing servers.",
	MCPCodexHint:   "Add to ~/.codex/config.toml (or config.toml in CODEX_HOME). Replace any existing motty entry.",
	MCPPreviewHint: "The preview hides the token. Copied configuration includes the real token.",
	MCPCopied:      "Copied",
	MCPCopyFailed:  "Copy failed",
	MCPToken:       "Access token",
	MCPTokenDesc:   "Clients must send it. Resetting it disconnects every client.",
	MCPReset:       "Reset token",
	MCPResetDone:   "Token reset",
}

var chineseAISettings = AISettingsMessages{
	Title:         "AI 助手",
	Description:   "内置助手的模型、安全限制和对外访问。",
	StoreErrorFmt: "无法打开助手的数据库：%s",

	ProvidersTitle:     "服务商",
	ProvidersEmpty:     "还没有服务商，添加一个即可使用助手。",
	ProviderAdd:        "添加服务商",
	ProviderEdit:       "编辑",
	ProviderDelete:     "删除",
	ProviderEnabled:    "启用",
	ProviderKeySaved:   "已保存密钥",
	ProviderNoKey:      "无密钥",
	ProviderOfficial:   "官方地址",
	ProviderKindOpenAI: "OpenAI 兼容",
	ProviderKindClaude: "Anthropic",

	FormAddTitle:      "添加服务商",
	FormEditTitle:     "编辑服务商",
	FormDescription:   "密钥只保存在这台电脑上，保存后不会再显示。",
	FormName:          "名称",
	FormNamePH:        "我的服务商",
	FormKind:          "接口类型",
	FormPreset:        "服务商",
	FormPresetCustom:  "自定义",
	FormBaseURL:       "基础地址",
	FormBaseURLOpenAI: "https://api.openai.com/v1（默认）",
	FormBaseURLClaude: "https://api.anthropic.com（默认）",
	FormKey:           "API 密钥",
	FormKeyPH:         "粘贴密钥",
	FormKeySaved:      "已保存密钥，留空表示保持不变。",
	FormSave:          "保存",
	FormAdd:           "添加",
	ErrFormName:       "请给服务商起个名字。",
	ErrFormBaseURL:    "基础地址必须以 http:// 或 https:// 开头。",
	ErrFormKey:        "使用官方地址需要填写 API 密钥。",
	ErrFormSaveFmt:    "无法保存：%s",
	ErrModelName:      "模型名称不能包含空格。",

	DeleteTitle:   "删除服务商？",
	DeleteBodyFmt: "将删除“%s”及其模型和已保存的密钥。",
	DeleteConfirm: "删除",

	ModelsTitle:      "模型",
	ModelsNoProvider: "添加服务商后即可管理其模型。",
	ModelsOfFmt:      "%s 的模型",
	ModelsDescFmt:    "共 %d 个模型，已启用 %d 个。默认模型用于回答新问题。",
	ModelsFetch:      "获取模型",
	ModelsFetching:   "正在获取...",
	ModelsCancel:     "取消",
	ModelsFetchedFmt: "找到 %d 个模型。",
	ModelsFailedFmt:  "无法获取模型：%s",
	ModelsEmpty:      "还没有模型。可以获取列表，或在下方输入模型名称。",
	ModelDefault:     "默认",
	ModelUse:         "设为默认",
	ModelEnabled:     "启用",
	ModelAddPH:       "模型名称",
	ModelAdd:         "添加模型",
	ModelPageFmt:     "第 %d-%d 项，共 %d 项",
	ModelPrev:        "上一页",
	ModelNext:        "下一页",

	BehaviourTitle:   "助手行为",
	AutoRun:          "无需确认直接执行",
	AutoRunDesc:      "不超过此风险等级的命令自行执行；高危命令始终先询问。",
	RiskLow:          "低",
	RiskMedium:       "中",
	RiskHigh:         "高",
	MaxSteps:         "最大步数",
	MaxStepsDesc:     "回答一个问题时，代理最多可调用工具的次数。",
	ContextLines:     "终端上下文",
	ContextLinesDesc: "在询问模式下，随问题发送的可见终端行数。",
	ContextOff:       "关闭",
	Redact:           "隐藏敏感信息",
	RedactDesc:       "发送给模型前，遮盖密码、密钥和令牌。",
	History:          "保存历史",
	HistoryDesc:      "对话只保存在这台电脑。",
	ClearHistory:     "清除历史",
	ClearHistoryDesc: "删除所有已保存的对话。",
	ClearHistoryDone: "历史已清除。",
	ClearHistoryBusy: "请等待当前请求结束。",
	ClearTitle:       "清除全部历史？",
	ClearBody:        "所有已保存的对话都会被删除，且无法恢复。",
	ClearConfirm:     "清除历史",

	MCPEnable:      "允许外部代理（MCP）",
	MCPEnableDesc:  "让 Claude Code 等工具通过本地服务使用你的会话。",
	MCPMode:        "访问级别",
	MCPModeDesc:    "“观察”只读；“询问”还可执行命令，每条都要先在这里批准。",
	MCPModeObserve: "观察",
	MCPModeAsk:     "询问",
	MCPEndpoint:    "地址",
	MCPNotRunning:  "未运行",
	MCPOff:         "已关闭",
	MCPCopy:        "复制配置",
	MCPConfig:      "客户端配置",
	MCPConfigDesc:  "选择要接入的客户端，访问权限对所有客户端统一生效。",
	MCPCopyURL:     "复制地址",
	MCPCopyToken:   "复制令牌",
	MCPClaudeHint:  "在项目终端执行，将 Motty 注册到 Claude Code。",
	MCPCursorHint:  "合并到项目的 .cursor/mcp.json，保留已有的其他服务器配置。",
	MCPCodexHint:   "添加到 ~/.codex/config.toml（或 CODEX_HOME 下的 config.toml），替换已有的 motty 配置项。",
	MCPPreviewHint: "预览已隐藏令牌，复制配置会包含真实令牌。",
	MCPCopied:      "已复制",
	MCPCopyFailed:  "复制失败",
	MCPToken:       "访问令牌",
	MCPTokenDesc:   "客户端必须携带它。重置后所有客户端都会断开。",
	MCPReset:       "重置令牌",
	MCPResetDone:   "令牌已重置",
}
