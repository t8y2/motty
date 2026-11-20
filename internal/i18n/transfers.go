package i18n

// TransferCenterMessages describes the window-wide upload and download history.
type TransferCenterMessages struct {
	Close                                                                                                                 string
	Title, Description, Empty, EmptyFiltered                                                                              string
	EmptyHint, EmptyFilteredHint                                                                                          string
	All, Active, Waiting, Ended, Clear, Remove, CancelAll                                                                 string
	Upload, Download, RemoteCopy, To, From, Queued, Running, Finalizing, Paused, Completed, Failed, Canceled, Interrupted string
	InterruptedHint                                                                                                       string
	SummaryFmt, Idle, ProgressFmt, FilesFmt, TimeFmt                                                                      string
	Retry, Replace, Resume, Skip, Conflict                                                                                string
	ShowInFolder, ShowInFolderFailed, ShowInFolderMissing                                                                 string
	Hosts, AllHosts, ScopeFmt, CancelVisible, ClearVisible, RetryFailedFmt, ConflictCountFmt                              string
	ConfirmBatchFmt, BatchScopeFmt                                                                                        string
	ReplaceAll, SkipAll                                                                                                   string
	RunningCountFmt, WaitingCountFmt, RemainingFmt, DurationFmt                                                           string
	PauseAll, ResumeAll, PauseVisible, ResumeVisible                                                                      string
	Direction, AllDirections, Search, Details, FileName, Size, Speed, Elapsed                                             string
	LocalPath, RemotePath, SourcePath, TargetPath, QueuedAt, StartedAt, FinishedAt, Error                                 string
}
