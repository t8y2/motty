package i18n

type MonitorMessages struct {
	MoveLeft, MoveRight                                                        string
	AvailableMemory, LoadAverages, Uptime                                      string
	UnsupportedPlatform                                                        string
	PrimaryDisk                                                                string
	PartialCoresFmt                                                            string
	Overview, CoresTitleFmt, CoresMoreFmt, DiskSection, NetworkSection         string
	NoCores, Unavailable, LoadUnavailable, LoadShortFmt                        string
	IntervalLabels                                                             [3]string
	Title                                                                      string
	CPU, Memory, Disks, Network                                                string
	LoadFmt, UptimeFmt, CPUCountFmt                                            string
	MoreDisksFmt, AllNetworks, Hardware, NoHardware                            string
	GPUMemory, Utilization, Power, Temperature, Processes, NoProcesses         string
	NoDisks, WaitingDetails, DetailsUnavailable                                string
	Pause, Resume, Paused, Updated, Interval, IntervalFmt, Retry, NotConnected string
	Collapse                                                                   string
	Logins, NoLogins, LoginsUnavailable, LoginsTruncated                       string
}

var chineseMonitor = MonitorMessages{
	MoveLeft: "将监控移到左侧", MoveRight: "将监控移到右侧",
	AvailableMemory: "可用内存", LoadAverages: "负载 · 1 / 5 / 15 分钟", Uptime: "运行时长",
	UnsupportedPlatform: "当前监控支持 Linux、macOS 和 Windows，此主机暂不支持。",
	PrimaryDisk:         "主磁盘",
	PartialCoresFmt:     "查看已采集的 %d / %d 核",
	Overview:            "概览", CoresTitleFmt: "CPU 核心分布（%d核）", CoresMoreFmt: "显示全部 %d 核", DiskSection: "磁盘挂载分区", NetworkSection: "网络接口",
	NoCores: "主机未提供核心读数", Unavailable: "不可用", LoadUnavailable: "负载不可用", LoadShortFmt: "负载 %.2f",
	IntervalLabels: [3]string{"1 秒", "2 秒", "5 秒"},
	Title:          "监控",
	CPU:            "CPU", Memory: "内存", Disks: "磁盘", Network: "网络",
	LoadFmt: "%.2f / %.2f / %.2f", UptimeFmt: "%d天%d小时", CPUCountFmt: "%d 逻辑核",
	MoreDisksFmt: "更多（%d）", AllNetworks: "总流量", Hardware: "硬件信息", NoHardware: "未获取到加速卡指标",
	GPUMemory: "显存", Utilization: "负载", Power: "功耗", Temperature: "温度", Processes: "进程 / 显存", NoProcesses: "无占用进程",
	NoDisks: "没有可用的磁盘信息", WaitingDetails: "正在读取主机信息…", DetailsUnavailable: "部分主机信息暂不可用，稍后自动重试",
	Pause: "暂停监控", Resume: "继续监控", Paused: "已暂停", Updated: "自动刷新 · 刚刚更新", Interval: "刷新间隔", IntervalFmt: "%d 秒", Retry: "重试", NotConnected: "连接到服务器后可查看监控。",
	Collapse: "收起",
	Logins:   "登录会话 (who)", NoLogins: "系统未报告登录会话", LoginsUnavailable: "无法读取登录会话", LoginsTruncated: "仅显示前 32 条会话",
}

var englishMonitor = MonitorMessages{
	MoveLeft: "Move monitor left", MoveRight: "Move monitor right",
	AvailableMemory: "Available memory", LoadAverages: "Load · 1 / 5 / 15 min", Uptime: "Uptime",
	UnsupportedPlatform: "Monitoring supports Linux, macOS and Windows. This host is not supported yet.",
	PrimaryDisk:         "System disk",
	PartialCoresFmt:     "Show %d / %d available cores",
	Overview:            "Overview", CoresTitleFmt: "CPU cores (%d)", CoresMoreFmt: "Show all %d cores", DiskSection: "Disk volumes", NetworkSection: "Network interfaces",
	NoCores: "Per-core readings unavailable", Unavailable: "Unavailable", LoadUnavailable: "Load unavailable", LoadShortFmt: "Load %.2f",
	IntervalLabels: [3]string{"1s", "2s", "5s"},
	Title:          "Monitor",
	CPU:            "CPU", Memory: "Memory", Disks: "Disks", Network: "Network",
	LoadFmt: "%.2f / %.2f / %.2f", UptimeFmt: "%dd %dh", CPUCountFmt: "%d vCPU",
	MoreDisksFmt: "More (%d)", AllNetworks: "Total traffic", Hardware: "Hardware", NoHardware: "No accelerator metrics available",
	GPUMemory: "VRAM", Utilization: "Load", Power: "Power", Temperature: "Temp", Processes: "Processes / VRAM", NoProcesses: "No compute processes",
	NoDisks: "No disk information available", WaitingDetails: "Reading host information…", DetailsUnavailable: "Some host information is unavailable. Retrying automatically.",
	Pause: "Pause monitoring", Resume: "Resume monitoring", Paused: "Paused", Updated: "Auto refresh · just updated", Interval: "Refresh interval", IntervalFmt: "%ds", Retry: "Retry", NotConnected: "Connect to a server to view monitoring.",
	Collapse: "Collapse",
	Logins:   "Login sessions (who)", NoLogins: "No login sessions reported", LoginsUnavailable: "Login sessions unavailable", LoginsTruncated: "Showing the first 32 sessions",
}
