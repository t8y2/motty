package i18n

type WinSCPImportMessages struct {
	Title, PickFile, SummaryFmt, SkippedFmt string
}

var englishWinSCPImport = WinSCPImportMessages{
	Title:      "WinSCP Site Import",
	PickFile:   "Choose the WinSCP INI file",
	SummaryFmt: "Imported %d WinSCP sites and %d key references. Skipped %d duplicates.",
	SkippedFmt: "Skipped %d unsupported or invalid WinSCP sites.",
}

var chineseWinSCPImport = WinSCPImportMessages{
	Title:      "WinSCP 站点导入",
	PickFile:   "选择 WinSCP INI 文件",
	SummaryFmt: "已导入 %d 个 WinSCP 站点和 %d 个密钥引用，跳过 %d 个重复站点。",
	SkippedFmt: "已跳过 %d 个不受支持或无效的 WinSCP 站点。",
}

var traditionalWinSCPImport = WinSCPImportMessages{
	Title:      "WinSCP 站點匯入",
	PickFile:   "選擇 WinSCP INI 檔案",
	SummaryFmt: "已匯入 %d 個 WinSCP 站點和 %d 個金鑰參照，略過 %d 個重複站點。",
	SkippedFmt: "已略過 %d 個不支援或無效的 WinSCP 站點。",
}
