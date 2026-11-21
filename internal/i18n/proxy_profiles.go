package i18n

// ProxyProfileMessages are the texts of the saved outbound proxy profiles: the
// Settings group, the dialog that edits one, the confirmation before one is
// deleted, and the choice in the host form that makes a host use one.
type ProxyProfileMessages struct {
	Group          string
	RowDescription string
	Add            string
	Edit           string
	Delete         string
	UsedByFmt      string
	UsedByNone     string
	// RowFmt says what a profile points at: its kind and address.
	RowFmt             string
	DialogAddTitle     string
	DialogEditTitle    string
	Name               string
	NamePlaceholder    string
	Kind               string
	Address            string
	AddressPlaceholder string
	User               string
	UserPlaceholder    string
	Password           string
	PasswordKeep       string
	// PasswordFromProfile says where a profile-carried route keeps its password.
	PasswordFromProfile   string
	TestTarget            string
	TestTargetPlaceholder string
	Test                  string
	Testing               string
	TestEndpointOK        string
	TestOKFmt             string
	TestFailedFmt         string
	Save                  string
	DeleteTitle           string
	DeleteFmt             string
	DeleteInUseFmt        string
	ErrorName             string
	ErrorNameTaken        string
	ErrorAddress          string
	ErrorTestTarget       string
	ErrorLogin            string
	ErrorTooMany          string
	ErrorVaultFmt         string
	// Choice names the host form's outbound proxy kind that uses a profile.
	Choice string
	// ProfileLabel labels the host form's profile select.
	ProfileLabel string
	// Manual is the profile select's first choice: fill the route in by hand.
	Manual string
	// ChoiceFmt names a profile in the host form's select: name, kind, address.
	ChoiceFmt string
	// HostDescriptionFmt describes a host carried by a profile.
	HostDescriptionFmt string
	// HostMissing says a host names a profile that no longer exists.
	HostMissing string
}

var englishProxyProfiles = ProxyProfileMessages{
	Group:                 "Proxy profiles",
	RowDescription:        "Save a proxy once and let several hosts use it. Change it here and every host that uses it follows. Choose one in the host's Outbound proxy.",
	Add:                   "Add proxy profile",
	Edit:                  "Edit",
	Delete:                "Delete",
	UsedByFmt:             "Used by %d hosts",
	UsedByNone:            "Not used by any host",
	RowFmt:                "%s at %s",
	DialogAddTitle:        "Add proxy profile",
	DialogEditTitle:       "Edit proxy profile",
	Name:                  "Name",
	NamePlaceholder:       "e.g. Office SOCKS",
	Kind:                  "Proxy type",
	Address:               "Proxy address (host:port)",
	AddressPlaceholder:    "127.0.0.1:1080",
	User:                  "User name (optional)",
	UserPlaceholder:       "Proxy login",
	Password:              "Password (optional)",
	PasswordKeep:          "Leave empty to keep the saved one",
	PasswordFromProfile:   "Saved in the proxy profile",
	TestTarget:            "Test target (optional)",
	TestTargetPlaceholder: "host:port — blank checks the proxy port only",
	Test:                  "Test",
	Testing:               "Testing…",
	TestEndpointOK:        "The proxy port answered.",
	TestOKFmt:             "The proxy carried a connection to %s.",
	TestFailedFmt:         "The proxy test failed: %s",
	Save:                  "Save",
	DeleteTitle:           "Delete proxy profile",
	DeleteFmt:             "Delete the proxy profile %q? Its saved password is removed and cannot be recovered.",
	DeleteInUseFmt:        "Hosts using it: %d. They will connect directly.",
	ErrorName:             "Enter a name.",
	ErrorNameTaken:        "A proxy profile with this name already exists.",
	ErrorAddress:          "Enter the proxy as host:port, with a port from 1 to 65535.",
	ErrorTestTarget:       "Enter the test target as host:port.",
	ErrorLogin:            "The user name and password may be 255 characters at most.",
	ErrorTooMany:          "There are too many proxy profiles.",
	ErrorVaultFmt:         "Could not save the password: %s",
	Choice:                "Saved profile",
	ProfileLabel:          "Proxy profile",
	Manual:                "Manual route",
	ChoiceFmt:             "%s · %s at %s",
	HostDescriptionFmt:    "Profile %s: %s at %s carries this host's first connection.",
	HostMissing:           "The saved proxy profile is missing; this host connects directly.",
}

var chineseProxyProfiles = ProxyProfileMessages{
	Group:                 "代理档案",
	RowDescription:        "保存一份代理配置，供多台主机使用。在这里修改后，所有使用它的主机自动生效。在主机的“出站代理”中选用。",
	Add:                   "添加代理档案",
	Edit:                  "编辑",
	Delete:                "删除",
	UsedByFmt:             "被 %d 台主机使用",
	UsedByNone:            "没有主机使用",
	RowFmt:                "%s 地址 %s",
	DialogAddTitle:        "添加代理档案",
	DialogEditTitle:       "编辑代理档案",
	Name:                  "名称",
	NamePlaceholder:       "例如：办公网 SOCKS",
	Kind:                  "代理类型",
	Address:               "代理地址（主机:端口）",
	AddressPlaceholder:    "127.0.0.1:1080",
	User:                  "用户名（可选）",
	UserPlaceholder:       "代理登录名",
	Password:              "密码（可选）",
	PasswordKeep:          "留空则保持不变",
	PasswordFromProfile:   "保存在代理档案中",
	TestTarget:            "测试目标（可选）",
	TestTargetPlaceholder: "主机:端口 — 留空只检测代理端口",
	Test:                  "测试",
	Testing:               "正在测试…",
	TestEndpointOK:        "代理端口已响应。",
	TestOKFmt:             "代理已连通目标 %s。",
	TestFailedFmt:         "代理测试失败：%s",
	Save:                  "保存",
	DeleteTitle:           "删除代理档案",
	DeleteFmt:             "删除代理档案“%s”？密码会从密码库中移除，无法恢复。",
	DeleteInUseFmt:        "使用它的主机：%d 台，之后将直接连接。",
	ErrorName:             "请输入名称。",
	ErrorNameTaken:        "已存在同名的代理档案。",
	ErrorAddress:          "请输入主机:端口形式的代理地址，端口范围为 1 到 65535。",
	ErrorTestTarget:       "请输入主机:端口形式的测试目标。",
	ErrorLogin:            "用户名和密码最多 255 个字符。",
	ErrorTooMany:          "代理档案过多。",
	ErrorVaultFmt:         "无法保存密码：%s",
	Choice:                "代理档案",
	ProfileLabel:          "代理档案",
	Manual:                "手动配置",
	ChoiceFmt:             "%s · %s 地址 %s",
	HostDescriptionFmt:    "档案“%s”：%s %s 承载这台主机的第一条连接。",
	HostMissing:           "代理档案已不存在；这台主机将直接连接。",
}

var traditionalProxyProfiles = ProxyProfileMessages{
	Group:                 "代理檔案",
	RowDescription:        "儲存一份代理設定，供多台主機使用。在這裡修改後，所有使用它的主機自動生效。在主機的「出站代理」中選用。",
	Add:                   "新增代理檔案",
	Edit:                  "編輯",
	Delete:                "刪除",
	UsedByFmt:             "被 %d 台主機使用",
	UsedByNone:            "沒有主機使用",
	RowFmt:                "%s 位址 %s",
	DialogAddTitle:        "新增代理檔案",
	DialogEditTitle:       "編輯代理檔案",
	Name:                  "名稱",
	NamePlaceholder:       "例如：辦公室 SOCKS",
	Kind:                  "代理類型",
	Address:               "代理位址（主機:連接埠）",
	AddressPlaceholder:    "127.0.0.1:1080",
	User:                  "使用者名稱（選填）",
	UserPlaceholder:       "代理登入名稱",
	Password:              "密碼（選填）",
	PasswordKeep:          "留空則維持不變",
	PasswordFromProfile:   "儲存在代理檔案中",
	TestTarget:            "測試目標（選填）",
	TestTargetPlaceholder: "主機:連接埠 — 留空只檢查代理連接埠",
	Test:                  "測試",
	Testing:               "正在測試…",
	TestEndpointOK:        "代理連接埠已回應。",
	TestOKFmt:             "代理已連通目標 %s。",
	TestFailedFmt:         "代理測試失敗：%s",
	Save:                  "儲存",
	DeleteTitle:           "刪除代理檔案",
	DeleteFmt:             "刪除代理檔案「%s」？密碼會從密碼庫中移除，無法復原。",
	DeleteInUseFmt:        "使用它的主機：%d 台，之後將直接連線。",
	ErrorName:             "請輸入名稱。",
	ErrorNameTaken:        "已存在同名的代理檔案。",
	ErrorAddress:          "請輸入主機:連接埠形式的代理位址，連接埠範圍為 1 到 65535。",
	ErrorTestTarget:       "請輸入主機:連接埠形式的測試目標。",
	ErrorLogin:            "使用者名稱和密碼最多 255 個字元。",
	ErrorTooMany:          "代理檔案過多。",
	ErrorVaultFmt:         "無法儲存密碼：%s",
	Choice:                "代理檔案",
	ProfileLabel:          "代理檔案",
	Manual:                "手動設定",
	ChoiceFmt:             "%s · %s 位址 %s",
	HostDescriptionFmt:    "檔案「%s」：%s %s 承載這台主機的第一條連線。",
	HostMissing:           "代理檔案已不存在；這台主機將直接連線。",
}
