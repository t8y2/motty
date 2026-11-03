package i18n

// SessionLogsMessages are the texts of the local session-log page: the journal
// of the connections the workbench opened, how each one ended, and what may be
// done with the records. Only connection metadata is stored, so nothing here
// talks about terminal contents, recordings or replay.
type SessionLogsMessages struct {
	ApplyRange                                                                               string
	DateToday, DateYesterday, DateEarlier                                                    string
	CountSummaryFmt, TimeCustom, DateFrom, DateThrough, InvalidDateRange, CustomRangeHintFmt string
	FilterSort, SortNewest, SortOldest, SortLongest, SortShortest                            string
	FieldStopped                                                                             string
	Title                                                                                    string
	Description                                                                              string

	// The search box and the four filters. The labels are what assistive
	// technology reads on a trigger whose visible text is the current choice.
	Search, SearchPlaceholder      string
	FilterTime, FilterStatus       string
	FilterProtocol, FilterFavorite string
	TimeAll, TimeToday             string
	Time7Days, Time30Days          string
	FavoriteAny, FavoriteOnly      string
	StatusAll, ProtocolAll         string

	// Stored statuses, protocols and the result codes a record may end with.
	StatusConnecting, StatusActive        string
	StatusReconnecting, StatusEnded       string
	StatusFailed, StatusInterrupted       string
	StatusTrackingStopped                 string
	ProtocolSSH, ProtocolLocal            string
	ProtocolTelnet, ProtocolSerial        string
	ProtocolVNC, ProtocolRDP              string
	ReasonNone, ReasonUserClosed          string
	ReasonConnectFailed, ReasonAuthFailed string
	ReasonHostKeyRefused                  string
	ReasonCanceled, ReasonTimeout         string
	ReasonNetworkLost, ReasonShellFailed  string
	ReasonTrackingOff                     string
	ReasonProcessRestart, ReasonUnknown   string

	// ListScroll names the table to assistive technology; it must not read like
	// the page title, or the two are indistinguishable in the accessible tree.
	ListScroll string

	// The table: the columns and the way a row reads.
	ColumnTarget, ColumnWhen       string
	ColumnProtocol, ColumnStatus   string
	ColumnDuration, ColumnFavorite string
	DurationOngoing                string

	// Counts, loading and the page's states.
	CountFmt, CountFilteredFmt   string
	LoadMore                     string
	Loading, LoadingHint         string
	Empty, EmptyHint             string
	NoMatches, NoMatchesHint     string
	ClearFilters                 string
	Unavailable, UnavailableHint string
	ListFailed, ListFailedHint   string

	// The detail panel.
	Details, DetailsHint                 string
	CloseDetail, BackToList              string
	FieldStarted, FieldEnded             string
	FieldTarget, FieldAccount            string
	FieldAddress, FieldProtocol          string
	FieldStatus, FieldDuration           string
	FieldReconnects, FieldResult         string
	FieldFavorite                        string
	Pending, Yes, No                     string
	Events, EventsCountFmt               string
	EventsTruncatedFmt                   string
	EventsEmpty, NoMoreInformation       string
	EventConnected, EventShellOpened     string
	EventShellFailed, EventDisconnected  string
	EventFailed, EventReconnectScheduled string
	EventReconnectStarted                string
	EventReconnectExhausted              string
	EventReconnectCanceled               string
	EventTrackingStopped                 string
	EventAttemptFmt                      string
	OpenSession, SessionClosed           string

	// Actions, their confirmations and their results.
	Favorite, Unfavorite                    string
	Delete, DeleteTitle                     string
	DeletePromptFmt, DeleteConfirm          string
	Deleted                                 string
	ClearUnfavorited, ClearUnfavoritedTitle string
	ClearUnfavoritedPrompt                  string
	ClearUnfavoritedConfirm                 string
	ClearedUnfavorited                      string
	ClearAll, ClearAllTitle                 string
	ClearAllPrompt, ClearAllConfirm         string
	ClearedAll                              string
	FavoritedStatus, UnfavoritedStatus      string
	WriteFailed                             string

	// Settings > Security: the local capture switch and what the journal holds.
	SettingsGroup                                                              string
	SettingsCaptureRow                                                         string
	SettingsCaptureDescription                                                 string
	SettingsStorageRow                                                         string
	SettingsStorageDescription                                                 string
	SettingsStorageFmt                                                         string
	SettingsStorageUnavailable                                                 string
	EventStarted, EventHostKeyPrompt, EventAuthenticating                      string
	EventFinished, EventRecovered, ResultSummaryFmt                            string
	FieldRecovered, DurationRecoveredFmt, FieldHostname, FieldPort, FieldShell string
	FilterOutcome, OutcomeAll, OutcomeNormal, OutcomeAbnormal                  string
	Reopen, ChooseHost, LocateHost, CopyMetadata                               string
	NewLocal                                                                   string
	More, ClearSelectionFmt                                                    string
	TargetChangedFmt, TargetChangedAgain, MetadataCopied, MetadataCopyFailed   string
}

var chineseSessionLogs = SessionLogsMessages{
	ApplyRange: "应用范围",
	DateToday:  "今天", DateYesterday: "昨天", DateEarlier: "更早",
	CountSummaryFmt: "已加载 %d / %d 条 · 全部 %d", TimeCustom: "自定义日期", DateFrom: "开始日期", DateThrough: "结束日期（含当天）", InvalidDateRange: "请输入有效的 YYYY-MM-DD 日期，开始日期不能晚于结束日期。", CustomRangeHintFmt: "日期范围：%s 至 %s",
	FilterSort: "会话排序", SortNewest: "最新优先", SortOldest: "最早优先", SortLongest: "时长降序", SortShortest: "时长升序",
	FieldStopped:      "记录停止",
	More:              "更多会话日志操作",
	ClearSelectionFmt: "%s：本机日志库中本次选定的 %d 条记录，含 %d 条收藏。范围不受当前筛选影响。\n确认后只删除本次快照；后来新增的记录保留。清除未收藏时，后来被收藏的记录也会保留。\n活动连接不会关闭；删除后该会话不自动补录。命令历史、录制和诊断日志不受影响。",
	NewLocal:          "新建本地终端",
	Reopen:            "再次连接", ChooseHost: "选择主机", LocateHost: "定位主机", CopyMetadata: "复制元数据摘要",
	TargetChangedFmt: "连接目标已改变。\n记录：%s\n当前：%s\n使用当前主机配置创建新会话？", TargetChangedAgain: "目标再次发生变化，请取消后重新确认。",
	MetadataCopied: "已复制会话元数据", MetadataCopyFailed: "无法复制会话元数据",
	FilterOutcome: "按结果筛选", OutcomeAll: "全部结果", OutcomeNormal: "正常结束", OutcomeAbnormal: "异常结束",
	EventStarted: "开始会话", EventHostKeyPrompt: "等待主机密钥确认", EventAuthenticating: "认证中",
	EventFinished: "会话结束", EventRecovered: "恢复时发现会话中断", ResultSummaryFmt: "依据记录结果：%s",
	FieldRecovered: "恢复发现", DurationRecoveredFmt: "%s（截至恢复）", FieldHostname: "主机名", FieldPort: "端口", FieldShell: "Shell",
	Title:       "会话日志",
	Description: "记录从工作台发起连接的主机、开始时间与结束方式。只保存在本机，不含终端内容。",

	Search:            "搜索会话",
	SearchPlaceholder: "搜索主机名、地址或账号",
	FilterTime:        "按时间范围筛选",
	FilterStatus:      "按状态筛选",
	FilterProtocol:    "按协议筛选",
	FilterFavorite:    "只看收藏",
	TimeAll:           "全部时间",
	TimeToday:         "今天",
	Time7Days:         "近 7 天",
	Time30Days:        "近 30 天",
	FavoriteAny:       "全部会话",
	FavoriteOnly:      "只看收藏",
	StatusAll:         "全部状态",
	ProtocolAll:       "全部协议",

	StatusConnecting:      "连接中",
	StatusActive:          "已连接",
	StatusReconnecting:    "重连中",
	StatusEnded:           "已结束",
	StatusFailed:          "失败",
	StatusInterrupted:     "已中断",
	StatusTrackingStopped: "已停止记录",
	ProtocolSSH:           "SSH",
	ProtocolLocal:         "本地 Shell",
	ProtocolTelnet:        "Telnet",
	ProtocolSerial:        "串口",
	ProtocolVNC:           "VNC",
	ProtocolRDP:           "RDP",
	ReasonNone:            "没有记录原因",
	ReasonUserClosed:      "由你关闭",
	ReasonConnectFailed:   "无法建立连接",
	ReasonAuthFailed:      "认证失败",
	ReasonHostKeyRefused:  "主机密钥被拒绝",
	ReasonCanceled:        "已取消",
	ReasonTimeout:         "连接超时",
	ReasonNetworkLost:     "连接中断",
	ReasonShellFailed:     "Shell 打开失败",
	ReasonTrackingOff:     "记录已停止",
	ReasonProcessRestart:  "因程序重启而中断",
	ReasonUnknown:         "原因不可用",

	ListScroll:      "会话日志列表",
	ColumnTarget:    "目标",
	ColumnWhen:      "开始时间",
	ColumnProtocol:  "协议",
	ColumnStatus:    "状态",
	ColumnDuration:  "时长",
	ColumnFavorite:  "收藏",
	DurationOngoing: "进行中",

	CountFmt:         "%d 条会话",
	CountFilteredFmt: "%d 条匹配会话",
	LoadMore:         "加载更多",
	Loading:          "正在读取会话…",
	LoadingHint:      "正在读取本机会话日志。",
	Empty:            "还没有会话记录",
	EmptyHint:        "从工作台发起的连接会记录在这里，并标明开始与结束方式。只在本机保存元数据。",
	NoMatches:        "没有符合筛选条件的会话",
	NoMatchesHint:    "可以换个搜索词，或清除筛选条件。",
	ClearFilters:     "清除筛选",
	Unavailable:      "会话记录不可用",
	UnavailableHint:  "无法打开本机会话日志。连接仍可正常使用；在日志恢复前不会记录新会话。",
	ListFailed:       "无法读取会话记录",
	ListFailedHint:   "本机会话日志返回错误，记录没有被改动。",

	Details:                 "会话详情",
	DetailsHint:             "此会话记录的生命周期事件",
	CloseDetail:             "关闭详情",
	BackToList:              "返回列表",
	FieldStarted:            "开始",
	FieldEnded:              "结束",
	FieldTarget:             "目标",
	FieldAccount:            "账号",
	FieldAddress:            "地址",
	FieldProtocol:           "协议",
	FieldStatus:             "状态",
	FieldDuration:           "时长",
	FieldReconnects:         "重连次数",
	FieldResult:             "结果",
	FieldFavorite:           "收藏",
	Pending:                 "进行中",
	Yes:                     "是",
	No:                      "否",
	Events:                  "事件",
	EventsCountFmt:          "共 %d 条事件",
	EventsTruncatedFmt:      "保留 %d 条事件（含最终结果，部分中间事件已省略）",
	EventsEmpty:             "没有记录生命周期事件",
	NoMoreInformation:       "没有为此会话保存更多信息。",
	EventConnected:          "已连接",
	EventShellOpened:        "Shell 已打开",
	EventShellFailed:        "Shell 打开失败",
	EventDisconnected:       "连接断开",
	EventFailed:             "连接失败",
	EventReconnectScheduled: "已安排自动重连",
	EventReconnectStarted:   "自动重连开始",
	EventReconnectExhausted: "自动重连已用尽",
	EventReconnectCanceled:  "自动重连已取消",
	EventTrackingStopped:    "停止记录元数据",
	EventAttemptFmt:         "第 %d 次",
	OpenSession:             "打开会话",
	SessionClosed:           "该会话已关闭",

	Favorite:                "收藏",
	Unfavorite:              "取消收藏",
	Delete:                  "删除",
	DeleteTitle:             "删除会话记录",
	DeletePromptFmt:         "删除 %s 的会话记录？其生命周期事件也会一起删除。命令历史、手动录制和诊断日志不受影响。",
	DeleteConfirm:           "删除",
	Deleted:                 "已删除会话记录",
	ClearUnfavorited:        "清除未收藏",
	ClearUnfavoritedTitle:   "清除未收藏的记录",
	ClearUnfavoritedPrompt:  "删除本日志中所有未收藏的会话记录？收藏的记录会保留。命令历史、手动录制和诊断日志不受影响。",
	ClearUnfavoritedConfirm: "清除未收藏",
	ClearedUnfavorited:      "已清除未收藏的会话记录",
	ClearAll:                "清除全部",
	ClearAllTitle:           "清除全部记录",
	ClearAllPrompt:          "删除本日志中的所有会话记录，包括收藏的记录？命令历史、手动录制和诊断日志不受影响。",
	ClearAllConfirm:         "清除全部",
	ClearedAll:              "已清除全部会话记录",
	FavoritedStatus:         "已收藏",
	UnfavoritedStatus:       "已取消收藏",
	WriteFailed:             "无法修改该记录。",

	SettingsGroup:              "会话日志",
	SettingsCaptureRow:         "保留本机会话日志",
	SettingsCaptureDescription: "记录在这台电脑上连接了哪台主机、何时开始、如何结束。不保存终端内容或密码。",
	SettingsStorageRow:         "本机记录",
	SettingsStorageDescription: "查看本机保存的会话记录及占用空间。",
	SettingsStorageFmt:         "%d 条记录 · 占用 %s",
	SettingsStorageUnavailable: "会话日志数据库无法打开，记录功能当前不可用。",
}

var englishSessionLogs = SessionLogsMessages{
	ApplyRange: "Apply range",
	DateToday:  "Today", DateYesterday: "Yesterday", DateEarlier: "Earlier",
	CountSummaryFmt: "Loaded %d / %d · Total %d", TimeCustom: "Custom dates", DateFrom: "Start date", DateThrough: "End date (inclusive)", InvalidDateRange: "Enter valid YYYY-MM-DD dates with the start on or before the end.", CustomRangeHintFmt: "Date range: %s through %s",
	FilterSort: "Sort sessions", SortNewest: "Newest first", SortOldest: "Oldest first", SortLongest: "Longest first", SortShortest: "Shortest first",
	FieldStopped:      "Capture ended",
	More:              "More session log actions",
	ClearSelectionFmt: "%s: %d records selected from this computer's journal, including %d favorites, across all filters.\nOnly this selection will be deleted. New records are kept; clearing unfavorited records also protects entries favorited later.\nLive connections stay open; deleted sessions are not recorded again. Command history, recordings and diagnostics are unaffected.",
	NewLocal:          "New local terminal",
	Reopen:            "Connect again", ChooseHost: "Choose host", LocateHost: "Locate host", CopyMetadata: "Copy metadata summary",
	TargetChangedFmt: "The target has changed.\nRecorded: %s\nCurrent: %s\nCreate a new session with the current host settings?", TargetChangedAgain: "The target changed again. Cancel and review it again.",
	MetadataCopied: "Session metadata copied", MetadataCopyFailed: "Could not copy session metadata",
	FilterOutcome: "Filter by outcome", OutcomeAll: "All outcomes", OutcomeNormal: "Ended normally", OutcomeAbnormal: "Ended abnormally",
	EventStarted: "Session started", EventHostKeyPrompt: "Waiting for host key verification", EventAuthenticating: "Authenticating",
	EventFinished: "Session ended", EventRecovered: "Interrupted session detected on recovery", ResultSummaryFmt: "From recorded result: %s",
	FieldRecovered: "Recovered", DurationRecoveredFmt: "%s (until recovery)", FieldHostname: "Hostname", FieldPort: "Port", FieldShell: "Shell",
	Title:       "Session Logs",
	Description: "Connections opened from the workbench, when they started and how they ended. Stored locally, without terminal contents.",

	Search:            "Search sessions",
	SearchPlaceholder: "Search host name, address or account",
	FilterTime:        "Filter by time range",
	FilterStatus:      "Filter by status",
	FilterProtocol:    "Filter by protocol",
	FilterFavorite:    "Show favorites only",
	TimeAll:           "All time",
	TimeToday:         "Today",
	Time7Days:         "7 days",
	Time30Days:        "30 days",
	FavoriteAny:       "All sessions",
	FavoriteOnly:      "Favorites only",
	StatusAll:         "All statuses",
	ProtocolAll:       "All protocols",

	StatusConnecting:      "Connecting",
	StatusActive:          "Connected",
	StatusReconnecting:    "Reconnecting",
	StatusEnded:           "Ended",
	StatusFailed:          "Failed",
	StatusInterrupted:     "Interrupted",
	StatusTrackingStopped: "Recording stopped",
	ProtocolSSH:           "SSH",
	ProtocolLocal:         "Local shell",
	ProtocolTelnet:        "Telnet",
	ProtocolSerial:        "Serial",
	ProtocolVNC:           "VNC",
	ProtocolRDP:           "RDP",
	ReasonNone:            "No reason recorded",
	ReasonUserClosed:      "Closed by you",
	ReasonConnectFailed:   "Could not connect",
	ReasonAuthFailed:      "Authentication failed",
	ReasonHostKeyRefused:  "Host key rejected",
	ReasonCanceled:        "Canceled",
	ReasonTimeout:         "Timed out",
	ReasonNetworkLost:     "Connection lost",
	ReasonShellFailed:     "Shell failed to open",
	ReasonTrackingOff:     "Recording stopped",
	ReasonProcessRestart:  "Interrupted by a restart",
	ReasonUnknown:         "Reason unavailable",

	ListScroll:      "Session log list",
	ColumnTarget:    "Target",
	ColumnWhen:      "Started",
	ColumnProtocol:  "Protocol",
	ColumnStatus:    "Status",
	ColumnDuration:  "Duration",
	ColumnFavorite:  "Favorite",
	DurationOngoing: "In progress",

	CountFmt:         "%d sessions",
	CountFilteredFmt: "%d matching sessions",
	LoadMore:         "Load more",
	Loading:          "Reading sessions…",
	LoadingHint:      "Reading the local session journal.",
	Empty:            "No sessions recorded yet",
	EmptyHint:        "Connections you open from the workbench appear here with how and when they ended. Only metadata is stored, on this computer.",
	NoMatches:        "No sessions match these filters",
	NoMatchesHint:    "Try another search, or clear the filters.",
	ClearFilters:     "Clear filters",
	Unavailable:      "Session records are unavailable",
	UnavailableHint:  "The local journal could not be opened. Connections still work; new sessions are not recorded until it is available.",
	ListFailed:       "Could not read the session records",
	ListFailedHint:   "The local journal returned an error. Nothing was changed.",

	Details:                 "Session details",
	DetailsHint:             "Lifecycle events recorded for this session",
	CloseDetail:             "Close details",
	BackToList:              "Back to the list",
	FieldStarted:            "Started",
	FieldEnded:              "Ended",
	FieldTarget:             "Target",
	FieldAccount:            "Account",
	FieldAddress:            "Address",
	FieldProtocol:           "Protocol",
	FieldStatus:             "Status",
	FieldDuration:           "Duration",
	FieldReconnects:         "Reconnects",
	FieldResult:             "Result",
	FieldFavorite:           "Favorite",
	Pending:                 "In progress",
	Yes:                     "Yes",
	No:                      "No",
	Events:                  "Events",
	EventsCountFmt:          "%d events",
	EventsTruncatedFmt:      "%d events retained, including the final result; intermediate events omitted",
	EventsEmpty:             "No lifecycle events recorded",
	NoMoreInformation:       "No more information is stored for this session.",
	EventConnected:          "Connected",
	EventShellOpened:        "Shell opened",
	EventShellFailed:        "Shell failed to open",
	EventDisconnected:       "Disconnected",
	EventFailed:             "Failed",
	EventReconnectScheduled: "Reconnect scheduled",
	EventReconnectStarted:   "Reconnect started",
	EventReconnectExhausted: "Reconnects exhausted",
	EventReconnectCanceled:  "Reconnect canceled",
	EventTrackingStopped:    "Metadata recording stopped",
	EventAttemptFmt:         "attempt %d",
	OpenSession:             "Open session",
	SessionClosed:           "That session is no longer open",

	Favorite:                "Favorite",
	Unfavorite:              "Unfavorite",
	Delete:                  "Delete",
	DeleteTitle:             "Delete session record",
	DeletePromptFmt:         "Delete the record for %s? Its lifecycle events are deleted too. Command history, manual recordings and the diagnostic log are not affected.",
	DeleteConfirm:           "Delete",
	Deleted:                 "Session record deleted",
	ClearUnfavorited:        "Clear unfavorited",
	ClearUnfavoritedTitle:   "Clear unfavorited records",
	ClearUnfavoritedPrompt:  "Delete every unfavorited session record in this journal? Favorites are kept. Command history, manual recordings and the diagnostic log are not affected.",
	ClearUnfavoritedConfirm: "Clear unfavorited",
	ClearedUnfavorited:      "Unfavorited session records cleared",
	ClearAll:                "Clear all",
	ClearAllTitle:           "Clear all records",
	ClearAllPrompt:          "Delete every session record in this journal, including favorites? Command history, manual recordings and the diagnostic log are not affected.",
	ClearAllConfirm:         "Clear all",
	ClearedAll:              "All session records cleared",
	FavoritedStatus:         "Marked as favorite",
	UnfavoritedStatus:       "Favorite removed",
	WriteFailed:             "The record could not be changed.",

	SettingsGroup:              "Session logs",
	SettingsCaptureRow:         "Keep local session logs",
	SettingsCaptureDescription: "Record which host a session reached, when it started and how it ended on this computer. Terminal contents and passwords are never saved.",
	SettingsStorageRow:         "Records",
	SettingsStorageDescription: "View locally stored session records and disk usage.",
	SettingsStorageFmt:         "%d records · %s",
	SettingsStorageUnavailable: "The session log database cannot be opened, so recording is unavailable.",
}
