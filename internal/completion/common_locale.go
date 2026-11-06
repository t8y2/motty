package completion

type commonDescription struct {
	name        string
	simplified  string
	traditional string
}

var commonDescriptions = [...]commonDescription{
	{"cat", "显示文件内容", "顯示檔案內容"},
	{"ls", "列出目录内容", "列出目錄內容"},
	{"cd", "切换当前目录", "切換目前目錄"},
	{"cp", "复制文件或目录", "複製檔案或目錄"},
	{"mv", "移动或重命名文件", "移動或重新命名檔案"},
	{"rm", "删除文件或目录", "刪除檔案或目錄"},
	{"mkdir", "创建目录", "建立目錄"},
	{"chmod", "修改文件权限", "修改檔案權限"},
	{"chown", "修改文件所有者", "修改檔案擁有者"},
	{"grep", "搜索匹配的文本行", "搜尋符合條件的文字行"},
	{"sed", "按规则编辑文本流", "依規則編輯文字串流"},
	{"awk", "按模式处理文本", "依模式處理文字"},
	{"find", "查找文件和目录", "尋找檔案和目錄"},
	{"head", "显示文件开头", "顯示檔案開頭"},
	{"tail", "显示文件末尾", "顯示檔案結尾"},
	{"sort", "对文本行排序", "排序文字行"},
	{"uniq", "处理相邻重复行", "處理相鄰重複行"},
	{"cut", "提取文本列", "擷取文字欄位"},
	{"wc", "统计行、词和字节数", "統計行、詞與位元組數"},
	{"tee", "同时写入文件和标准输出", "同時寫入檔案與標準輸出"},
	{"xargs", "将输入转换成命令参数", "將輸入轉成指令參數"},
	{"du", "统计文件或目录占用空间", "統計檔案或目錄佔用空間"},
	{"df", "查看文件系统容量", "查看檔案系統容量"},
	{"tar", "打包或解包归档", "封存或解開封存檔"},
	{"gzip", "压缩文件", "壓縮檔案"},
	{"ssh", "连接远程 SSH 主机", "連接遠端 SSH 主機"},
	{"scp", "通过 SSH 复制文件", "透過 SSH 複製檔案"},
	{"rsync", "同步文件和目录", "同步檔案與目錄"},
	{"curl", "传输 URL 数据", "傳輸 URL 資料"},
	{"wget", "下载网络资源", "下載網路資源"},
	{"ping", "检查主机网络连通性", "檢查主機網路連通性"},
	{"ip", "管理网络接口与路由", "管理網路介面與路由"},
	{"ss", "查看网络套接字", "查看網路通訊端"},
	{"netstat", "查看网络连接与统计", "查看網路連線與統計"},
	{"ps", "列出进程", "列出處理程序"},
	{"kill", "向进程发送信号", "傳送訊號給處理程序"},
	{"top", "查看实时进程资源", "查看即時處理程序資源"},
	{"systemctl", "管理 systemd 服务", "管理 systemd 服務"},
	{"journalctl", "查看 systemd 日志", "查看 systemd 日誌"},
	{"apt", "管理 Debian 软件包", "管理 Debian 軟體套件"},
	{"dnf", "管理 RPM 软件包", "管理 RPM 軟體套件"},
	{"yum", "管理 RPM 软件包", "管理 RPM 軟體套件"},
	{"git", "管理 Git 版本历史", "管理 Git 版本歷史"},
	{"docker", "管理容器和镜像", "管理容器與映像檔"},
	{"kubectl", "管理 Kubernetes 资源", "管理 Kubernetes 資源"},
	{"make", "执行构建规则", "執行建置規則"},
	{"gcc", "编译 C 程序", "編譯 C 程式"},
	{"go", "构建和管理 Go 项目", "建置與管理 Go 專案"},
	{"cargo", "构建和管理 Rust 项目", "建置與管理 Rust 專案"},
	{"rustc", "编译 Rust 程序", "編譯 Rust 程式"},
	{"java", "运行 Java 程序", "執行 Java 程式"},
	{"javac", "编译 Java 源码", "編譯 Java 原始碼"},
	{"npm", "管理 Node.js 软件包", "管理 Node.js 套件"},
	{"yarn", "管理 JavaScript 依赖", "管理 JavaScript 相依套件"},
	{"pnpm", "管理 JavaScript 依赖", "管理 JavaScript 相依套件"},
	{"node", "运行 Node.js 程序", "執行 Node.js 程式"},
	{"python", "运行 Python 程序", "執行 Python 程式"},
	{"pip", "管理 Python 软件包", "管理 Python 套件"},
	{"brew", "管理 Homebrew 软件包", "管理 Homebrew 套件"},
	{"pwsh", "启动 PowerShell", "啟動 PowerShell"},
	{"powershell", "启动 Windows PowerShell", "啟動 Windows PowerShell"},
	{"cmd", "启动 Windows 命令解释器", "啟動 Windows 命令直譯器"},
	{"dir", "列出目录内容", "列出目錄內容"},
	{"Get-Command", "查找 PowerShell 命令", "尋找 PowerShell 指令"},
	{"Get-ChildItem", "列出 PowerShell 路径中的项目", "列出 PowerShell 路徑中的項目"},
}

var commonDescriptionIndex = func() map[string]int {
	index := make(map[string]int, len(commonDescriptions))
	for position, description := range commonDescriptions {
		index[description.name] = position
	}
	return index
}()

var commonOptionDescriptions = map[string][2]string{
	"awk -F":                   {"指定字段分隔符", "指定欄位分隔符"},
	"awk -f":                   {"从文件读取 AWK 程序", "從檔案讀取 AWK 程式"},
	"cat -b":                   {"为非空行编号", "為非空行編號"},
	"cat -e":                   {"显示不可见字符和行尾", "顯示不可見字元和行尾"},
	"cat -n":                   {"为所有行编号", "為所有行編號"},
	"cat -s":                   {"压缩连续空行", "壓縮連續空行"},
	"cat -t":                   {"显示不可见字符和制表符", "顯示不可見字元和定位字元"},
	"cat -u":                   {"关闭输出缓冲", "關閉輸出緩衝"},
	"cat -v":                   {"显示不可见字符", "顯示不可見字元"},
	"ls -a":                    {"包含隐藏文件", "包含隱藏檔案"},
	"ls -h":                    {"以易读单位显示大小", "以易讀單位顯示大小"},
	"ls -l":                    {"使用详细列表格式", "使用詳細清單格式"},
	"kill -l":                  {"列出可用信号", "列出可用訊號"},
	"kill -s":                  {"指定要发送的信号", "指定要傳送的訊號"},
	"ip addr":                  {"查看或修改接口地址", "查看或修改介面位址"},
	"ip link":                  {"查看或修改网络接口", "查看或修改網路介面"},
	"ip route":                 {"查看或修改路由表", "查看或修改路由表"},
	"ss --all":                 {"显示所有套接字", "顯示所有通訊端"},
	"ss -a":                    {"显示所有套接字", "顯示所有通訊端"},
	"ss --listening":           {"仅显示监听套接字", "僅顯示監聽通訊端"},
	"ss -l":                    {"仅显示监听套接字", "僅顯示監聽通訊端"},
	"ss --numeric":             {"不解析服务名", "不解析服務名稱"},
	"ss -n":                    {"不解析服务名", "不解析服務名稱"},
	"netstat --listening":      {"显示监听连接", "顯示監聽連線"},
	"netstat -l":               {"显示监听连接", "顯示監聽連線"},
	"netstat --tcp":            {"显示 TCP 连接", "顯示 TCP 連線"},
	"netstat -t":               {"显示 TCP 连接", "顯示 TCP 連線"},
	"netstat --udp":            {"显示 UDP 连接", "顯示 UDP 連線"},
	"netstat -u":               {"显示 UDP 连接", "顯示 UDP 連線"},
	"yum install":              {"安装软件包", "安裝軟體套件"},
	"yum list":                 {"列出软件包", "列出軟體套件"},
	"yum remove":               {"移除软件包", "移除軟體套件"},
	"yum update":               {"更新软件包", "更新軟體套件"},
	"gcc -c":                   {"仅编译，不链接", "只編譯，不連結"},
	"gcc -o":                   {"指定输出文件", "指定輸出檔案"},
	"pwsh -NoProfile":          {"启动时不加载配置脚本", "啟動時不載入設定腳本"},
	"pwsh -Command":            {"执行命令后退出", "執行指令後結束"},
	"powershell -NoProfile":    {"启动时不加载配置脚本", "啟動時不載入設定腳本"},
	"powershell -Command":      {"执行命令后退出", "執行指令後結束"},
	"cmd /C":                   {"执行命令后退出", "執行指令後結束"},
	"cmd /K":                   {"执行命令后保持窗口", "執行指令後保留視窗"},
	"dir /A":                   {"按属性筛选项目", "依屬性篩選項目"},
	"dir /B":                   {"只显示名称", "只顯示名稱"},
	"Get-Command -Name":        {"按名称查找命令", "依名稱尋找指令"},
	"Get-Command -CommandType": {"按命令类型筛选", "依指令類型篩選"},
	"Get-ChildItem -Path":      {"列出指定路径", "列出指定路徑"},
	"Get-ChildItem -Recurse":   {"包含子目录", "包含子目錄"},
	"tail -f":                  {"持续显示新增内容", "持續顯示新增內容"},
	"tail -n":                  {"指定末尾行数", "指定結尾行數"},
}

func CommonPriority(name string) int {
	if position, ok := commonDescriptionIndex[name]; ok {
		return position
	}
	return len(commonDescriptions)
}

func LocalizedDescription(language, root, token string) string {
	if language != "zh-CN" && language != "zh-TW" {
		return ""
	}
	position := 0
	if language == "zh-TW" {
		position = 1
	}
	if token != "" {
		return commonOptionDescriptions[root+" "+token][position]
	}
	if index, ok := commonDescriptionIndex[root]; ok {
		if position == 1 {
			return commonDescriptions[index].traditional
		}
		return commonDescriptions[index].simplified
	}
	return ""
}
