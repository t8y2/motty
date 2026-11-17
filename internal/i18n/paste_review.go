package i18n

// PasteReviewMessages holds the multi-line paste confirmation's texts: the
// dialog that reviews text containing line breaks before it reaches a shell.
type PasteReviewMessages struct {
	Title       string
	SummaryFmt  string // line count, then character count
	EditorLabel string
	Hint        string
	TooLargeFmt string // the full line count
	Whole       string
	Lines       string
	Stale       string
	Stopped     string
}
