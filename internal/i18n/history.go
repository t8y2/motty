package i18n

// HistoryMessages are the texts of the command history: the picker, its
// Settings rows and the per-host switch.
type HistoryMessages struct {
	Title       string
	Placeholder string
	ScopeHost   string
	ScopeAll    string
	UsedFmt     string
	Hint        string
	Empty       string
	EmptyNone   string
	EmptyOff    string

	SuggestRow          string
	SuggestDescription  string
	SettingsRow         string
	SettingsDescription string
	ClearRow            string
	ClearDescription    string
	Clear               string
	ClearTitle          string
	ClearConfirm        string
	HostNoRecord        string

	StatusClearing        string
	StatusCleared         string
	StatusClearFailedFmt  string
	StatusUnavailableFmt  string
	StatusNoTerminal      string
	StatusNotConnected    string
	StatusRemovedFmt      string
	StatusRemoveFailedFmt string
}
