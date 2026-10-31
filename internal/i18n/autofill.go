package i18n

// AutofillMessages are the texts of the autofill rules: the Settings group, the
// dialog that edits a rule and the confirmation before a rule first answers.
type AutofillMessages struct {
	Group              string
	Row                string
	RowDescription     string
	Add                string
	DeleteTitle        string
	DeleteFmt          string
	DeleteTextNote     string
	PresetSudo         string
	PresetSu           string
	PresetSudoName     string
	PresetSuName       string
	Edit               string
	Delete             string
	TurnOn             string
	TurnOff            string
	SendPassword       string
	SendOTP            string
	SendText           string
	ScopeAll           string
	ScopeFmt           string
	StateOn            string
	StateOff           string
	WithEnter          string
	DialogAddTitle     string
	DialogEditTitle    string
	Name               string
	NamePlaceholder    string
	Pattern            string
	PatternPlaceholder string
	Send               string
	FixedText          string
	FixedTextHint      string
	Enter              string
	Scope              string
	ScopePlaceholder   string
	Enabled            string
	Save               string

	ErrorName        string
	ErrorPattern     string
	ErrorTooLong     string
	ErrorMatchesAll  string
	ErrorInvalidFmt  string
	ErrorTooMany     string
	ErrorFixedText   string
	ErrorScopeFmt    string
	ErrorVaultLocked string
	ErrorVaultFmt    string

	ConfirmTitle    string
	ConfirmFmt      string
	ConfirmSendsFmt string
	ConfirmLine     string
	ConfirmAllow    string
	StatusFiredFmt  string
	StatusNoSecret  string
	StatusLockedFmt string
	StatusDeclined  string
}
