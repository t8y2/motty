package i18n

// SSHSyncMessages are the texts of the two-way sync with the OpenSSH config:
// its Settings rows, what it tells the user it did and what it could not do.
type SSHSyncMessages struct {
	Group string

	ToggleRow         string
	ToggleDescription string
	FileRow           string
	FileChoose        string
	FileReset         string
	FilePickerTitle   string
	NowRow            string
	NowNever          string
	NowButton         string

	ResultTitle string
	Disabled    string
	UpToDate    string
	SummaryFmt  string
	FailedFmt   string

	WarnSharedFmt        string
	WarnOverriddenFmt    string
	WarnEndpointFmt      string
	WarnGitAliasFmt      string
	WarnJumpFmt          string
	WarnMissingFmt       string
	WarnMarkers          string
	WarnConflictFmt      string
	WarnRenameFmt        string
	WarnWriteFmt         string
	WarnDetachedFmt      string
	WarnChangedMeanwhile string
	WarnFileMissing      string
	WarnNoneLeftFmt      string
}

var englishSSHSync = SSHSyncMessages{
	Group: "SSH config",

	ToggleRow:         "Sync with SSH config",
	ToggleDescription: "Keeps your OpenSSH config and these hosts in step both ways. Hosts made here go in a marked block of the file, and a copy of the file is kept before the first change.",
	FileRow:           "SSH config file",
	FileChoose:        "Choose…",
	FileReset:         "Default",
	FilePickerTitle:   "Choose the SSH config file to sync",
	NowRow:            "Sync now",
	NowNever:          "Not synced yet.",
	NowButton:         "Sync now",

	ResultTitle: "SSH Config Sync",
	Disabled:    "SSH config sync is off.",
	UpToDate:    "SSH config is up to date.",
	SummaryFmt:  "SSH config: %d changed here, %d changed there, %d added here, %d added there, %d no longer synced.",
	FailedFmt:   "SSH config sync failed: %s",

	WarnSharedFmt:        "%s shares a block with other hosts in the file, so it can only be changed there.",
	WarnOverriddenFmt:    "%s: an earlier setting in the file overrides %s.",
	WarnEndpointFmt:      "Skipped %s: another host here already has its address, user and port.",
	WarnGitAliasFmt:      "Skipped %s: it is a Git remote alias, not a host to open.",
	WarnJumpFmt:          "Could not map the ProxyJump of %s.",
	WarnMissingFmt:       "%d synced hosts are missing here, so none were removed from the SSH config.",
	WarnMarkers:          "Motty's block in the SSH config has damaged markers, so no host was added to it.",
	WarnConflictFmt:      "%s changed on both sides; the SSH config's value was kept.",
	WarnRenameFmt:        "Could not rename %s in the SSH config: %s",
	WarnWriteFmt:         "Could not change %s in the SSH config: %s",
	WarnDetachedFmt:      "%s is gone from the SSH config; it stays here and is no longer synced.",
	WarnChangedMeanwhile: "The SSH config changed while syncing; it will be tried again.",
	WarnFileMissing:      "The SSH config file is missing, so nothing was changed. It is made again when it is next needed.",
	WarnNoneLeftFmt:      "None of the %d synced hosts is in the SSH config any more, so they were kept as they are.",
}

var chineseSSHSync = SSHSyncMessages{
	Group: "SSH 配置",

	ToggleRow:         "与 SSH 配置同步",
	ToggleDescription: "让 OpenSSH 配置与这里的主机双向保持一致。在这里创建的主机会写入文件中带标记的区块,首次修改前会保留一份文件副本。",
	FileRow:           "SSH 配置文件",
	FileChoose:        "选择…",
	FileReset:         "默认",
	FilePickerTitle:   "选择要同步的 SSH 配置文件",
	NowRow:            "立即同步",
	NowNever:          "尚未同步。",
	NowButton:         "立即同步",

	ResultTitle: "SSH 配置同步",
	Disabled:    "SSH 配置同步已关闭。",
	UpToDate:    "SSH 配置已是最新。",
	SummaryFmt:  "SSH 配置:这里更新 %d 项,配置文件更新 %d 项,这里新增 %d 台,配置文件新增 %d 台,%d 台不再同步。",
	FailedFmt:   "SSH 配置同步失败:%s",

	WarnSharedFmt:        "%s 与文件中的其他主机共用一个区块,只能在文件里修改。",
	WarnOverriddenFmt:    "%s:文件中靠前的设置覆盖了 %s。",
	WarnEndpointFmt:      "已跳过 %s:这里已有主机使用相同的地址、用户和端口。",
	WarnGitAliasFmt:      "已跳过 %s:它是 Git 远程别名,不是要打开的主机。",
	WarnJumpFmt:          "无法映射 %s 的 ProxyJump。",
	WarnMissingFmt:       "有 %d 台已同步的主机在这里找不到,因此没有从 SSH 配置中删除任何主机。",
	WarnMarkers:          "SSH 配置中 Motty 区块的标记已损坏,未添加任何主机。",
	WarnConflictFmt:      "%s 在两边都被修改过,已保留 SSH 配置中的值。",
	WarnRenameFmt:        "无法在 SSH 配置中重命名 %s:%s",
	WarnWriteFmt:         "无法在 SSH 配置中修改 %s:%s",
	WarnDetachedFmt:      "%s 已从 SSH 配置中删除;它保留在这里,不再同步。",
	WarnChangedMeanwhile: "同步时 SSH 配置发生了变化,将再试一次。",
	WarnFileMissing:      "SSH 配置文件不存在,因此没有做任何更改。需要时会重新创建。",
	WarnNoneLeftFmt:      "SSH 配置中已找不到任何一台已同步的主机(共 %d 台),因此保持原样。",
}
