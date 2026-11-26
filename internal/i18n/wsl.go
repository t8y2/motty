package i18n

// WSLMessages labels the saved WSL session form.
type WSLMessages struct {
	Distribution, DefaultDistribution                         string
	UseUser, User                                             string
	Directory, DirectoryHint, StartupCommand, Graphics        string
	ErrDistribution, ErrUser, ErrDirectory                    string
	ErrInvalidHost, ErrNoDefaultDistribution, ErrNeedsWindows string
}

var englishWSL = WSLMessages{
	Distribution: "Distribution", DefaultDistribution: "Default distribution",
	UseUser: "Specify a user", User: "Linux username",
	Directory: "Startup directory", DirectoryHint: "Home directory if empty",
	StartupCommand: "Startup command", Graphics: "Allow WSLg graphical apps",
	ErrDistribution: "Select a valid WSL distribution.", ErrUser: "Enter a valid Linux username.",
	ErrDirectory:             "Enter an absolute Linux path, such as /home/user/project.",
	ErrInvalidHost:           "This WSL session has an invalid distribution, user or startup directory.",
	ErrNoDefaultDistribution: "No default WSL distribution is registered.",
	ErrNeedsWindows:          "WSL sessions are only available on Windows.",
}

var chineseWSL = WSLMessages{
	Distribution: "发行版", DefaultDistribution: "默认发行版",
	UseUser: "指定用户", User: "Linux 用户名",
	Directory: "启动目录", DirectoryHint: "留空使用用户主目录",
	StartupCommand: "启动命令", Graphics: "允许使用 WSLg 图形应用",
	ErrDistribution: "请选择有效的 WSL 发行版。", ErrUser: "请输入有效的 Linux 用户名。",
	ErrDirectory:             "请输入 Linux 绝对路径，例如 /home/user/project。",
	ErrInvalidHost:           "此 WSL 会话的发行版、用户或启动目录无效。",
	ErrNoDefaultDistribution: "没有注册默认的 WSL 发行版。",
	ErrNeedsWindows:          "WSL 会话仅在 Windows 上可用。",
}

var traditionalWSL = WSLMessages{
	Distribution: "發行版本", DefaultDistribution: "預設發行版本",
	UseUser: "指定使用者", User: "Linux 使用者名稱",
	Directory: "啟動目錄", DirectoryHint: "留空時使用家目錄",
	StartupCommand: "啟動指令", Graphics: "允許使用 WSLg 圖形應用程式",
	ErrDistribution: "請選擇有效的 WSL 發行版本。", ErrUser: "請輸入有效的 Linux 使用者名稱。",
	ErrDirectory:             "請輸入 Linux 絕對路徑，例如 /home/user/project。",
	ErrInvalidHost:           "此 WSL 工作階段的發行版本、使用者或啟動目錄無效。",
	ErrNoDefaultDistribution: "尚未註冊預設的 WSL 發行版本。",
	ErrNeedsWindows:          "WSL 工作階段僅適用於 Windows。",
}
