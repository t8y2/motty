package i18n

// LocalProxyMessages describes the compact panel for exposing the local HTTP
// proxy through the current authenticated SSH connection.
type LocalProxyMessages struct {
	Title             string
	BackToTerminal    string
	ServerFmt         string
	PortLabel         string
	PortHint          string
	PortLockedHint    string
	ScopeWarning      string
	AccessWarning     string
	StartHint         string
	Starting          string
	ConnectedFmt      string
	Connected         string
	Start             string
	Test              string
	Testing           string
	TestSucceeded     string
	Stop              string
	StoppedFmt        string
	TerminalUsage     string
	TerminalHint      string
	EnableCopy        string
	DisableCopy       string
	SetupNotice       string
	RestoreNotice     string
	StoppedNotice     string
	StopHint          string
	More              string
	Less              string
	HowItWorks        string
	Flow              string
	ProtocolHint      string
	RemoteAddressFmt  string
	CopyUnavailable   string
	DockerTitle       string
	DockerHint        string
	DockerWarning     string
	DockerSteps       string
	DockerRestore     string
	DockerCopy        string
	DockerCopied      string
	DockerScope       string
	DockerConfigFmt   string
	DockerRestoreFmt  string
	GitSSHWarning     string
	CopyFailedFmt     string
	InvalidPort       string
	Unavailable       string
	ProxyDialFailed   string
	ForwardingRefused string
	BindingRejected   string
	SSHDisconnected   string
	ProxyAuthRequired string
	ProtocolMismatch  string
	TestFailed        string
	OpenDockerDocs    string
	OpenDocsFailed    string
}

var englishLocalProxy = LocalProxyMessages{
	Title:             "Use local proxy",
	BackToTerminal:    "Return to terminal",
	ServerFmt:         "Current server: %s",
	PortLabel:         "Local HTTP proxy port (127.0.0.1)",
	PortHint:          "Enter the HTTP proxy port on 127.0.0.1. The suggested port is not tested yet.",
	PortLockedHint:    "Stop the tunnel to change this port.",
	ScopeWarning:      "Motty opens the tunnel. Paste and run the setup command so tools in this terminal can use it.",
	AccessWarning:     "Other users and processes on this server may also use this proxy.",
	StartHint:         "Configure the terminal or Docker explicitly after the tunnel connects.",
	Starting:          "Connecting the tunnel…",
	ConnectedFmt:      "Tunnel connected: server 127.0.0.1:%d",
	Connected:         "Proxy tunnel is on",
	Start:             "Start & copy command",
	Test:              "Test connection",
	Testing:           "Testing through the tunnel…",
	TestSucceeded:     "Connection test succeeded for this attempt",
	Stop:              "Stop & copy restore",
	StoppedFmt:        "Tunnel stopped. Server configuration you applied may still point to port %d.",
	TerminalUsage:     "Proxy settings",
	TerminalHint:      "Run the command in sh, bash or zsh, then retry the download. Only tools configured to use this proxy will use it; other terminals and Docker need separate configuration. Keep the local proxy and SSH connected.",
	EnableCopy:        "Copy setup & return",
	DisableCopy:       "Copy restore command",
	SetupNotice:       "Setup command copied. Paste and run it in this terminal, then retry the download.",
	RestoreNotice:     "Restore command copied. Paste and run it to restore this terminal's previous proxy settings.",
	StoppedNotice:     "Proxy tunnel stopped. Restore command copied. Paste and run it to restore this terminal's proxy settings.",
	StopHint:          "Stopping interrupts downloads using this tunnel. Run the restore command afterward.",
	More:              "More options",
	Less:              "Fewer options",
	HowItWorks:        "How it works",
	Flow:              "Download tool → SSH tunnel → local proxy → internet",
	ProtocolHint:      "Use an HTTP-capable proxy port. VPN routing or a SOCKS-only port is insufficient. Tool settings and no_proxy/NO_PROXY may bypass the proxy; the local proxy's access policy applies.",
	RemoteAddressFmt:  "Server proxy: http://127.0.0.1:%d",
	CopyUnavailable:   "Clipboard is unavailable. Try copying again.",
	DockerTitle:       "Docker setup",
	DockerHint:        "Shell proxy variables do not configure image pulls. The Docker daemon makes those requests.",
	DockerWarning:     "The example uses sudo and restarts Docker, which may interrupt containers. Motty never runs these commands. Restore this configuration before stopping the tunnel.",
	DockerSteps:       "Systemd-managed Linux Docker",
	DockerRestore:     "Restore Docker configuration",
	DockerCopy:        "Copy command",
	DockerCopied:      "Command copied",
	DockerScope:       "Only for Docker Engine managed by systemd on Linux. Existing daemon.json or service configuration may take precedence, so inspect it before adding this drop-in. This example does not cover rootless Docker, Docker Desktop, or a remote Docker context.",
	DockerConfigFmt:   "HTTP_PROXY=http://127.0.0.1:%d\nHTTPS_PROXY=http://127.0.0.1:%d",
	DockerRestoreFmt:  "Remove /etc/systemd/system/docker.service.d/motty-proxy.conf only if you created it, then reload and restart Docker.",
	GitSSHWarning:     "HTTP proxy variables do not cover Git over SSH. Use an HTTPS repository URL or the tool's own configuration.",
	CopyFailedFmt:     "Could not copy: %v",
	InvalidPort:       "Enter a port from 1 to 65535.",
	Unavailable:       "This SSH session is not available.",
	ProxyDialFailed:   "No HTTP proxy is reachable on 127.0.0.1 at the selected port.",
	ForwardingRefused: "The server refused SSH remote forwarding.",
	BindingRejected:   "The remote listener cannot be verified as IPv4 loopback only. The tunnel was not started.",
	SSHDisconnected:   "The SSH connection was lost and the tunnel closed.",
	ProxyAuthRequired: "The local proxy requires authentication, which this version does not support.",
	ProtocolMismatch:  "The selected endpoint does not provide a compatible HTTP proxy.",
	TestFailed:        "The connection test failed through the tunnel.",
	OpenDockerDocs:    "Open Docker documentation",
	OpenDocsFailed:    "Could not open Docker documentation.",
}

var chineseLocalProxy = LocalProxyMessages{
	Title:             "通过本机代理",
	BackToTerminal:    "返回终端",
	ServerFmt:         "当前服务器：%s",
	PortLabel:         "本机 HTTP 代理端口（127.0.0.1）",
	PortHint:          "填写 127.0.0.1 上的 HTTP 代理端口。预填值尚未验证。",
	PortLockedHint:    "停止代理后可修改端口。",
	ScopeWarning:      "Motty 建立代理通道；粘贴执行配置命令后，当前终端的下载工具才会使用它。",
	AccessWarning:     "服务器上的其他用户和程序也可能使用此代理。",
	StartHint:         "隧道连接后，请按需配置终端或 Docker。",
	Starting:          "正在连接隧道…",
	ConnectedFmt:      "隧道已连接：服务器 127.0.0.1:%d",
	Connected:         "代理隧道已开启",
	Start:             "开启并复制命令",
	Test:              "测试连接",
	Testing:           "正在通过隧道测试…",
	TestSucceeded:     "本次连接测试成功",
	Stop:              "停止并复制撤销命令",
	StoppedFmt:        "隧道已停止。你在服务器上设置的配置可能仍指向端口 %d。",
	TerminalUsage:     "代理设置",
	TerminalHint:      "在 sh、bash 或 zsh 中执行配置命令后重试下载。只有配置了代理的工具才会使用；其他终端和 Docker 需单独配置。使用期间请保持本机代理和 SSH 连接。",
	EnableCopy:        "复制配置并返回终端",
	DisableCopy:       "复制撤销命令",
	SetupNotice:       "配置命令已复制，请在当前终端粘贴并执行，然后重试下载。",
	RestoreNotice:     "撤销命令已复制，请粘贴并执行，恢复当前终端原有的代理配置。",
	StoppedNotice:     "隧道已停止，撤销命令已复制。请粘贴并执行，恢复终端原有的代理配置。",
	StopHint:          "停止会中断经此隧道进行的下载，之后请执行撤销命令。",
	More:              "更多操作",
	Less:              "收起更多操作",
	HowItWorks:        "工作原理",
	Flow:              "下载工具 → SSH 隧道 → 本机代理 → 外网",
	ProtocolHint:      "需使用支持 HTTP 的代理端口，仅有 VPN 路由或 SOCKS5 端口不够。工具自身设置及 no_proxy/NO_PROXY 可能绕过代理；访问范围受本机代理规则限制。",
	RemoteAddressFmt:  "服务器代理：http://127.0.0.1:%d",
	CopyUnavailable:   "无法使用剪贴板，请重试复制。",
	DockerTitle:       "Docker 设置",
	DockerHint:        "终端代理变量不会配置镜像拉取；拉取请求由 Docker daemon 发出。",
	DockerWarning:     "示例会使用 sudo 并重启 Docker，可能中断容器。Motty 不会执行这些命令。停止隧道前请恢复此配置。",
	DockerSteps:       "systemd 管理的 Linux Docker",
	DockerRestore:     "恢复 Docker 配置",
	DockerCopy:        "复制命令",
	DockerCopied:      "命令已复制",
	DockerScope:       "仅适用于 Linux 上由 systemd 管理的 Docker Engine；已有 daemon.json 或服务配置可能优先，添加此 drop-in 前请先检查。不适用于 rootless Docker、Docker Desktop 或远程 Docker context。",
	DockerConfigFmt:   "示例环境变量：\nHTTP_PROXY=http://127.0.0.1:%d\nHTTPS_PROXY=http://127.0.0.1:%d",
	DockerRestoreFmt:  "仅删除你自己创建的 /etc/systemd/system/docker.service.d/motty-proxy.conf，然后重新加载并重启 Docker。",
	GitSSHWarning:     "HTTP 代理变量不会覆盖 Git SSH；请使用 HTTPS 仓库地址或该工具自身的代理配置。",
	CopyFailedFmt:     "复制失败：%v",
	InvalidPort:       "请输入 1 到 65535 之间的端口。",
	Unavailable:       "当前 SSH 会话不可用。",
	ProxyDialFailed:   "所选端口上没有可访问的本机 HTTP 代理。",
	ForwardingRefused: "服务器拒绝了 SSH 远程转发。",
	BindingRejected:   "无法确认远端监听仅绑定 IPv4 回环地址，隧道未启动。",
	SSHDisconnected:   "SSH 连接已断开，隧道已关闭。",
	ProxyAuthRequired: "本机代理要求身份验证，当前版本不支持。",
	ProtocolMismatch:  "所选端点不是兼容的 HTTP 代理。",
	TestFailed:        "经隧道进行的连接测试失败。",
	OpenDockerDocs:    "打开 Docker 文档",
	OpenDocsFailed:    "无法打开 Docker 文档。",
}
