package i18n

// HostsPanelMessages names the session manager docked beside the terminals.
type HostsPanelMessages struct {
	Toggle, Close, Resize, Manage string
}

var hostsPanelEnglish = HostsPanelMessages{
	Toggle: "Session manager", Close: "Close session manager", Resize: "Resize session manager", Manage: "Manage hosts",
}

var chineseHostsPanel = HostsPanelMessages{
	Toggle: "会话管理", Close: "关闭会话管理", Resize: "调整会话管理宽度", Manage: "管理主机",
}
