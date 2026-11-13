package i18n

// ActionFeedbackMessages explain failed foreground actions without exposing
// operating-system or parser diagnostics in a transient notice.
type ActionFeedbackMessages struct {
	FilePickerFailed          string
	FileMissing               string
	ReadDenied                string
	ReadFailed                string
	SaveFolderMissing         string
	SaveDenied                string
	SaveFailed                string
	ImportSSHConfigFailed     string
	ImportHostCatalogFailed   string
	ImportUpdateRequired      string
	ImportSessionsFailedFmt   string
	ImportFolderFailedFmt     string
	ImportPrivateKeyFailedFmt string
	BackupTooLarge            string
	NewWindowFailed           string
	InvalidConnection         string
}

var chineseActionFeedback = ActionFeedbackMessages{
	FilePickerFailed:          "无法打开文件选择窗口，请稍后重试。",
	FileMissing:               "找不到这个文件，请重新选择。",
	ReadDenied:                "没有权限读取这个文件，请选择其他文件或检查文件权限。",
	ReadFailed:                "无法读取这个文件，请确认文件可以正常打开后重试。",
	SaveFolderMissing:         "保存位置不存在，请重新选择文件夹。",
	SaveDenied:                "没有权限保存到这里，请选择其他位置。",
	SaveFailed:                "保存失败，请换个保存位置，或检查磁盘空间后重试。",
	ImportSSHConfigFailed:     "无法导入此 SSH 配置，请检查文件内容是否完整。",
	ImportHostCatalogFailed:   "无法导入此文件，请选择 Motty 导出的主机文件。",
	ImportUpdateRequired:      "此文件由更新版本的 Motty 导出，请更新 Motty 后再试。",
	ImportSessionsFailedFmt:   "无法导入这些连接，请选择 %s 导出的连接文件。",
	ImportFolderFailedFmt:     "无法导入这些会话，请选择保存 %s 会话的文件夹。",
	ImportPrivateKeyFailedFmt: "无法导入“%s”，请重新选择可读取的 SSH 私钥文件。",
	BackupTooLarge:            "备份文件过大，请选择其他备份。",
	NewWindowFailed:           "暂时无法打开新窗口，请稍后重试。",
	InvalidConnection:         "连接地址有误，请按 用户名@主机地址:端口 填写，例如 admin@192.168.1.10:22。",
}

var englishActionFeedback = ActionFeedbackMessages{
	FilePickerFailed:          "Could not open the file dialog. Please try again shortly.",
	FileMissing:               "This file could not be found. Please select it again.",
	ReadDenied:                "You do not have permission to read this file. Choose another file or check its permissions.",
	ReadFailed:                "Could not read this file. Check that it opens normally, then try again.",
	SaveFolderMissing:         "The save location no longer exists. Choose another folder.",
	SaveDenied:                "You do not have permission to save here. Choose another location.",
	SaveFailed:                "Could not save the file. Choose another location or check the available disk space.",
	ImportSSHConfigFailed:     "Could not import this SSH config. Check that the file is complete.",
	ImportHostCatalogFailed:   "Could not import this file. Choose a host file exported by Motty.",
	ImportUpdateRequired:      "This file was exported by a newer Motty version. Update Motty and try again.",
	ImportSessionsFailedFmt:   "Could not import these connections. Choose connection files exported by %s.",
	ImportFolderFailedFmt:     "Could not import these sessions. Choose the folder that holds your %s sessions.",
	ImportPrivateKeyFailedFmt: "Could not import “%s”. Select a readable SSH private key file.",
	BackupTooLarge:            "This backup file is too large. Choose another backup.",
	NewWindowFailed:           "Could not open a new window. Please try again shortly.",
	InvalidConnection:         "Check the connection address. Use user@host:port, for example admin@192.168.1.10:22.",
}
