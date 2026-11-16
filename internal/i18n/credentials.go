package i18n

// CredentialMessages are the texts of the shared passwords: the Settings group,
// the dialog that edits one, the confirmation before one is deleted, and the
// choice in the host form that makes a host use one.
type CredentialMessages struct {
	Group           string
	Row             string
	RowDescription  string
	Add             string
	Edit            string
	Delete          string
	UsedByFmt       string
	UsedByNone      string
	DialogAddTitle  string
	DialogEditTitle string
	Name            string
	NamePlaceholder string
	Password        string
	PasswordKeep    string
	Save            string
	DeleteTitle     string
	DeleteFmt       string
	DeleteInUseFmt  string
	ErrorName       string
	ErrorNameTaken  string
	ErrorPassword   string
	ErrorTooMany    string
	ErrorVaultFmt   string
	// FieldSource is the host form's choice of where the password comes from;
	// SourceOwn is its first item: the password kept with this host alone.
	FieldSource string
	SourceOwn   string
	// SourceMissing names a shared password the host asks for that is gone.
	SourceMissing string
	// StatusRefused is shown at the password prompt when a shared saved
	// password was refused: it is kept, because other hosts use it too.
	StatusRefused string
}

var englishCredentials = CredentialMessages{
	Group:           "Shared passwords",
	Row:             "Shared passwords",
	RowDescription:  "Keep a password once and let several hosts use it. Change it here and every host that uses it follows. Choose one in the host's Password source.",
	Add:             "Add shared password",
	Edit:            "Edit",
	Delete:          "Delete",
	UsedByFmt:       "Used by %d hosts",
	UsedByNone:      "Not used by any host",
	DialogAddTitle:  "Add shared password",
	DialogEditTitle: "Edit shared password",
	Name:            "Name",
	NamePlaceholder: "e.g. Internal servers",
	Password:        "Password",
	PasswordKeep:    "Leave empty to keep",
	Save:            "Save",
	DeleteTitle:     "Delete shared password",
	DeleteFmt:       "Delete the shared password %q? The password is removed from the vault and cannot be recovered.",
	DeleteInUseFmt:  "%d hosts use it and will ask for their password again.",
	ErrorName:       "Enter a name.",
	ErrorNameTaken:  "A shared password with this name already exists.",
	ErrorPassword:   "Enter a password.",
	ErrorTooMany:    "There are too many shared passwords.",
	ErrorVaultFmt:   "Could not save the password: %s",
	FieldSource:     "Password source",
	SourceOwn:       "This host only",
	SourceMissing:   "Missing shared password",
	StatusRefused:   "The shared password was refused. It was kept because other hosts use it; change it in Settings.",
}

var chineseCredentials = CredentialMessages{
	Group:           "共用密码",
	Row:             "共用密码",
	RowDescription:  "把密码统一保存一份，供多台主机使用。在这里修改后，所有使用它的主机自动生效。在主机的“密码来源”中选用。",
	Add:             "添加共用密码",
	Edit:            "编辑",
	Delete:          "删除",
	UsedByFmt:       "被 %d 台主机使用",
	UsedByNone:      "没有主机使用",
	DialogAddTitle:  "添加共用密码",
	DialogEditTitle: "编辑共用密码",
	Name:            "名称",
	NamePlaceholder: "例如：内网服务器",
	Password:        "密码",
	PasswordKeep:    "留空则保持不变",
	Save:            "保存",
	DeleteTitle:     "删除共用密码",
	DeleteFmt:       "删除共用密码“%s”？密码会从密码库中移除，无法恢复。",
	DeleteInUseFmt:  "有 %d 台主机在使用它，之后连接时需要重新输入密码。",
	ErrorName:       "请输入名称。",
	ErrorNameTaken:  "已存在同名的已保存密码。",
	ErrorPassword:   "请输入密码。",
	ErrorTooMany:    "共用密码过多。",
	ErrorVaultFmt:   "无法保存密码：%s",
	FieldSource:     "密码来源",
	SourceOwn:       "仅此主机",
	SourceMissing:   "共用密码已不存在",
	StatusRefused:   "共用密码被拒绝。因为其他主机也在使用，所以没有删除；请在设置中修改。",
}

var traditionalCredentials = CredentialMessages{
	Group:           "共用密碼",
	Row:             "共用密碼",
	RowDescription:  "把密碼統一儲存一份，供多台主機使用。在這裡修改後，所有使用它的主機自動生效。在主機的「密碼來源」中選用。",
	Add:             "新增共用密碼",
	Edit:            "編輯",
	Delete:          "刪除",
	UsedByFmt:       "被 %d 台主機使用",
	UsedByNone:      "沒有主機使用",
	DialogAddTitle:  "新增共用密碼",
	DialogEditTitle: "編輯共用密碼",
	Name:            "名稱",
	NamePlaceholder: "例如：內網伺服器",
	Password:        "密碼",
	PasswordKeep:    "留空則保持不變",
	Save:            "儲存",
	DeleteTitle:     "刪除共用密碼",
	DeleteFmt:       "刪除共用密碼「%s」？密碼會從密碼庫中移除，無法復原。",
	DeleteInUseFmt:  "有 %d 台主機在使用它，之後連線時需要重新輸入密碼。",
	ErrorName:       "請輸入名稱。",
	ErrorNameTaken:  "已存在同名的已儲存密碼。",
	ErrorPassword:   "請輸入密碼。",
	ErrorTooMany:    "共用密碼過多。",
	ErrorVaultFmt:   "無法儲存密碼：%s",
	FieldSource:     "密碼來源",
	SourceOwn:       "僅此主機",
	SourceMissing:   "共用密碼已不存在",
	StatusRefused:   "共用密碼被拒絕。因為其他主機也在使用，所以沒有刪除；請在設定中修改。",
}
