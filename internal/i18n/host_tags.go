package i18n

// HostTagMessages are the texts of the Settings group that manages host tags:
// every tag the hosts use and the built-in suggestions, each renamed or deleted
// across all hosts at once.
type HostTagMessages struct {
	Group            string
	Empty            string
	EmptyDescription string
	UsedByFmt        string
	UsedByNone       string
	BuiltIn          string
	Rename           string
	Delete           string
	RenameTitle      string
	RenameFmt        string
	RenameLabel      string
	Save             string
	DeleteTitle      string
	DeleteFmt        string
	DeleteInUseFmt   string
	RestoreRow       string
	RestoreFmt       string
	Restore          string
	StatusRenamedFmt string
	StatusDeletedFmt string
	StatusRestored   string
}

var englishHostTags = HostTagMessages{
	Group:            "Host tags",
	Empty:            "No tags",
	EmptyDescription: "Tags you give hosts are listed here.",
	UsedByFmt:        "Used by %d hosts",
	UsedByNone:       "Not used by any host",
	BuiltIn:          "Built-in suggestion",
	Rename:           "Rename",
	Delete:           "Delete",
	RenameTitle:      "Rename tag",
	RenameFmt:        "Every host tagged “%s” gets the new name. A name another tag already has merges the two.",
	RenameLabel:      "Name",
	Save:             "Save",
	DeleteTitle:      "Delete tag",
	DeleteFmt:        "Delete the tag “%s”? It is no longer suggested when you edit a host.",
	DeleteInUseFmt:   "It is taken off %d hosts; the hosts themselves are kept.",
	RestoreRow:       "Deleted suggestions",
	RestoreFmt:       "Suggest %s again when you edit a host.",
	Restore:          "Restore",
	StatusRenamedFmt: "Renamed the tag to “%s” on %d hosts",
	StatusDeletedFmt: "Deleted the tag “%s”",
	StatusRestored:   "Restored the built-in tag suggestions",
}

var chineseHostTags = HostTagMessages{
	Group:            "主机标签",
	Empty:            "暂无标签",
	EmptyDescription: "给主机加上的标签会列在这里。",
	UsedByFmt:        "%d 台主机在用",
	UsedByNone:       "没有主机使用",
	BuiltIn:          "内置建议",
	Rename:           "重命名",
	Delete:           "删除",
	RenameTitle:      "重命名标签",
	RenameFmt:        "所有带“%s”标签的主机都会改用新名称。改成已有的标签名会把两个标签合并。",
	RenameLabel:      "名称",
	Save:             "保存",
	DeleteTitle:      "删除标签",
	DeleteFmt:        "删除标签“%s”？编辑主机时不会再建议它。",
	DeleteInUseFmt:   "它会从 %d 台主机上移除，主机本身保留。",
	RestoreRow:       "已删除的建议",
	RestoreFmt:       "编辑主机时重新建议 %s。",
	Restore:          "恢复",
	StatusRenamedFmt: "已将 %[2]d 台主机的标签改为“%[1]s”",
	StatusDeletedFmt: "已删除标签“%s”",
	StatusRestored:   "已恢复内置标签建议",
}

var traditionalHostTags = HostTagMessages{
	Group:            "主機標籤",
	Empty:            "暫無標籤",
	EmptyDescription: "替主機加上的標籤會列在這裡。",
	UsedByFmt:        "%d 台主機使用中",
	UsedByNone:       "沒有主機使用",
	BuiltIn:          "內建建議",
	Rename:           "重新命名",
	Delete:           "刪除",
	RenameTitle:      "重新命名標籤",
	RenameFmt:        "所有帶「%s」標籤的主機都會改用新名稱。改成已有的標籤名稱會把兩個標籤合併。",
	RenameLabel:      "名稱",
	Save:             "儲存",
	DeleteTitle:      "刪除標籤",
	DeleteFmt:        "刪除標籤「%s」？編輯主機時不會再建議它。",
	DeleteInUseFmt:   "它會從 %d 台主機上移除，主機本身會保留。",
	RestoreRow:       "已刪除的建議",
	RestoreFmt:       "編輯主機時重新建議 %s。",
	Restore:          "還原",
	StatusRenamedFmt: "已將 %[2]d 台主機的標籤改為「%[1]s」",
	StatusDeletedFmt: "已刪除標籤「%s」",
	StatusRestored:   "已還原內建標籤建議",
}
