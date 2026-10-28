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
	Title              string
	// ToggleTip names the window-bar button; Toggle is the shortcut and command
	// palette entry.
	ToggleTip string
	Toggle    string
	Close     string

	NewConversation string
	History         string
	HistoryTitle    string
	HistoryEmpty    string
	HistoryOpen     string
	HistoryDelete   string
	HistoryRename   string

	// The rename dialog of a saved conversation.
	RenameTitle       string
	RenameField       string
	RenamePlaceholder string
	RenameSubmit      string
	RenameRequired    string

	UsageFmt      string
	UsageTotalFmt string

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

	EmptyTitle  string
	EmptyHint   string
	Example1    string
	Example2    string
	Example3    string
	ScrollToEnd string

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

	MenuAskSelection  string
	MenuExplainOutput string
	PromptSelectionFn string // introduces a quoted selection
	PromptExplain     string
}
