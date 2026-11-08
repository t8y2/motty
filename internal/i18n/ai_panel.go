package i18n

// AIPanelMessages are the texts of the assistant panel.
type AIPanelMessages struct {
	Target             string
	TargetChoose       string
	TargetSwitchedFmt  string
	TargetNone         string
	TargetConnected    string
	TargetDisconnected string
	TargetUnavailable  string
	TargetLocked       string
	TargetHint         string
	// TargetGroupFmt labels the row that ticks every terminal of a split tab;
	// tab; TargetLimitFmt says why one more connection was not added.
	TargetGroupFmt string
	TargetLimitFmt string
	Title          string
	// ToggleTip names the window-bar button; Toggle is the shortcut and command
	// palette entry.
	ToggleTip string
	Toggle    string
	Close     string

	NewConversation       string
	History               string
	HistoryTitle          string
	HistoryEmpty          string
	HistoryOpen           string
	HistoryDelete         string
	HistoryRename         string
	HistoryScrollbarLabel string

	// The rename dialog of a saved conversation.
	RenameTitle       string
	RenameField       string
	RenamePlaceholder string
	RenameSubmit      string
	RenameRequired    string

	UsageFmt                string
	UsageTotalFmt           string
	UsageCachedFmt          string
	RunWaiting              string
	RunWaitingLong          string
	RunAnswering            string
	RunToolFmt              string
	RunApproval             string
	RunStopping             string
	RunCompleted            string
	RunStopped              string
	RunElapsedFmt           string
	RunTotalTimeFmt         string
	RunUsageFmt             string
	RunUsageUnknown         string
	RunUsageIncomplete      string
	RunUsageConversationFmt string

	ModeAsk   string
	ModeAgent string
	// What the mode switch in the message box does when clicked, for each state.
	ModeHintAsk   string
	ModeHintAgent string

	SetupModel   string
	SetupSummary string

	ModelChooseFmt string // the model chip: the model and what pressing it does
	ResizePanel    string // the name of the edge that changes the panel's width
	ResizeComposer string // and of the one that changes the message box's height

	InputLabel          string
	InputPlaceholder    string
	InputPlaceholderAsk string
	Send                string
	Stop                string

	// Files and pictures attached to a message. The Fmt strings take the file's
	// name first.
	Attach                   string // the paperclip: its name and tooltip
	Upload                   string
	UploadPickerTitle        string
	UploadNeedsAgent         string
	AttachPickerTitle        string
	AttachDropHint           string // over the panel while files are dragged in
	AttachRemoveFmt          string
	AttachedLabel            string // the name of the row of attachments under a sent message
	AttachTooManyFmt         string // takes the most that can be attached
	AttachFolderFmt          string
	AttachEmptyFmt           string
	AttachTextTooBigFmt      string // takes the name and the size limit
	AttachReadTooLargeFmt    string // takes the name and the maximum file read size
	AttachTooLargeFmt        string
	AttachUnsupportedFmt     string
	AttachTotalTextFmt       string // takes the name and the size limit of all text
	AttachFailedFmt          string // takes the name and the system's reason
	PastedImageName          string // the name a picture pasted from the clipboard goes by
	PastedTextName           string // the name a long text pasted from the clipboard goes by
	AttachPastedTruncatedFmt string // takes the name, limit and truncated size when huge pasted text is capped
	MessageTooLargeFmt       string // takes the saved-message size limit
	HistorySaveFailedFmt     string // takes the system's reason

	EmptyTitle     string
	EmptyHint      string
	Example1       string
	Example2       string
	Example3       string
	ScrollToEnd    string
	ScrollbarLabel string

	// The empty panel while no terminal is open: what to do to give the assistant
	// something to look at, and questions that need no terminal.
	IdleTitle    string
	IdleHint     string
	IdleExample1 string
	IdleExample2 string
	IdleExample3 string

	Thinking    string
	ThinkingNow string // the same while the model is still at it
	ShowOutput  string
	HideOutput  string
	CopyCode    string
	CopyAnswer  string
	AnswerText  string // the selectable body of an assistant answer

	ToolListSessions     string
	ToolTerminalRead     string
	ToolTerminalExec     string
	ToolTerminalExecMany string
	ToolSFTPList         string
	ToolSFTPRead         string
	ToolSFTPWrite        string
	ToolSFTPRemove       string

	StatusRunning string
	StatusWaiting string
	StatusDone    string
	StatusFailed  string
	StatusDenied  string

	RiskLow      string
	RiskMedium   string
	RiskHigh     string
	RiskCritical string

	// The approval scope chip in the message box and the menu behind it: what
	// each scope lets run without asking, in this conversation only. The chip
	// carries its scope's name alone; the hint says what the name controls.
	AutoScopeHint       string
	AutoScopeBusy       string // why the chip waits while work is running
	AutoScopeLowDesc    string
	AutoScopeMediumDesc string
	AutoScopeHighDesc   string

	ReasoningEffort     string
	ReasoningEffortHint string
	ReasoningNone       string
	ReasoningMinimal    string
	ReasoningLow        string
	ReasoningMedium     string
	ReasoningHigh       string
	ReasoningXHigh      string
	ReasoningMax        string
	ReasoningDefault    string
	ReasoningAuto       string

	ApprovalTitle       string
	ApprovalMoreFmt     string // takes how many more are waiting
	ConfirmFmt          string
	ConfirmField        string
	Approve             string
	Decline             string
	ApprovalSafer       string
	EscalationTitle     string
	EscalationSummary   string
	EscalationApprove   string
	EscalationDecline   string
	EscalationCancelled string
	EscalationMissing   string
	NoticeStoreError    string
	// AnswerTruncated explains a reply the provider cut off at its output
	// limit, so a half answer is not read as a finished one.
	AnswerTruncated string

	MenuAskSelection  string
	MenuExplainOutput string
	PromptSelectionFn string // introduces a quoted selection
	PromptExplain     string
}
