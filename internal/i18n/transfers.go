package i18n

// TransferCenterMessages describes the window-wide upload and download history.
type TransferCenterMessages struct {
	Close                                                                                        string
	Title, Description, Empty, EmptyFiltered                                                     string
	All, Active, Ended, Clear, Remove, CancelAll                                                 string
	Upload, Download, To, From, Queued, Running, Finalizing, Paused, Completed, Failed, Canceled string
	SummaryFmt, Idle, ProgressFmt, FilesFmt, TimeFmt                                             string
	Retry, Replace, Resume, Skip, Conflict                                                       string
}
