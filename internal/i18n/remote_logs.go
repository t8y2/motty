package i18n

type RemoteLogMessages struct {
	Title, Paths, Add, Edit, Remove, RemovePrompt, Name, Path                  string
	Search, Pause, Resume, Live, Following, Paused, Connecting, Stopped, Retry string
	Expand, Restore, Close, NoPaths, SelectPath, NoMatches, Disconnected       string
	InvalidPath, DuplicatePath, PathLimit, PathChanged                         string
}

var remoteLogsEnglish = RemoteLogMessages{
	Title: "Server logs", Paths: "Saved paths", Add: "Add path", Edit: "Edit path", Remove: "Remove path",
	RemovePrompt: "Remove the saved path %s?", Name: "Name", Path: "Remote absolute path",
	Search: "Find in log", Pause: "Pause", Resume: "Resume", Live: "Live", Following: "Following latest", Paused: "Paused",
	Connecting: "Opening log…", Stopped: "Log stream ended", Retry: "Retry", Expand: "Expand", Restore: "Restore size", Close: "Close logs",
	NoPaths: "Save a path to get started.", SelectPath: "Select a saved path to read its log.",
	NoMatches: "No matches in the retained log.", Disconnected: "SSH disconnected",
	InvalidPath:   "Enter a name and an absolute path without control characters.",
	DuplicatePath: "This path is already saved.", PathLimit: "Up to 64 paths can be saved.",
	PathChanged: "The saved path changed. Reopen it and try again.",
}

var chineseRemoteLogs = RemoteLogMessages{
	Title: "服务器日志", Paths: "已保存路径", Add: "添加路径", Edit: "编辑路径", Remove: "移除路径",
	RemovePrompt: "移除已保存的路径 %s？", Name: "名称", Path: "远端绝对路径",
	Search: "搜索日志", Pause: "暂停", Resume: "继续", Live: "实时", Following: "跟随最新日志", Paused: "已暂停",
	Connecting: "正在打开日志…", Stopped: "日志流已结束", Retry: "重试", Expand: "展开", Restore: "还原大小", Close: "关闭日志",
	NoPaths: "添加一个路径即可开始。", SelectPath: "选择已保存的路径以读取日志。",
	NoMatches: "当前缓冲区中没有匹配项。", Disconnected: "SSH 已断开",
	InvalidPath:   "请输入名称和绝对路径，且不能包含控制字符。",
	DuplicatePath: "此路径已保存。", PathLimit: "最多可保存 64 个路径。",
	PathChanged: "已保存的路径发生了变化，请重新打开后再试。",
}
