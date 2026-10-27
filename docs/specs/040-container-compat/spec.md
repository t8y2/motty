# Motty 容器兼容与交互终端

Issue: https://github.com/t8y2/motty/issues/40

## 目标

沿用 Motty 的 Go + Viva 原生“远程工具 → 容器”页面，不增加图标。修复旧 Docker CLI 不识别 `{{.State}}` 时容器列表不可用的问题；为当前 SSH 主机上的运行中容器提供独立标签页中的交互 Shell，不把命令写入原终端。

用户追加：兼容性不限于“容器”标签，现有远程工具的进程、镜像、卷、网络和 Compose 标签也要逐项覆盖旧版命令；无法安全降级时明确报告能力缺失，不把错误显示成空表。

## 范围

- 现代 Docker 保留现有 `docker ps -a --format` 的五列路径。仅当失败输出明确表示 `.State` 字段不被模板识别时，改用 ID/Names/Image/Status 四列，并从 Status 推导机器状态。停止/启动/健康状态分别展示；两个模板都失败要报告错误，不能把失败当作没有容器。
- 保留当前容器表与刷新、日志及操作；镜像、卷、网络、Compose 只补读侧兼容，不改变既有写操作契约。错误响应区分 CLI 缺失、daemon 拒绝/不可达和正常空列表；不自动 sudo、不开放 TCP daemon、不安装远程软件。
- 进程：现代 `ps` 查询失败于不支持的选项/列时，有限尝试兼容查询，缺少指标不能捏造 CPU、内存或运行时长；没有可用的安全格式则保留真实“不支持”错误。
- 镜像、卷、网络：对旧 Docker 模板中不认识的可选字段按字段定向降级，保留资源的真实 ID/名称；不丢失用于已有删除确认的 Key。普通 CLI/daemon 错误不得触发版本回退。
- Compose：对缺少插件、不支持 `ls`、不支持 JSON 格式等给出明确可区分的能力提示；只有结构化结果可以可靠解析时才展示项目，禁止解析失败变成空项目列表。现有写操作保持原确认逻辑，不新增兼容性的写操作。
- 当前页面的容器/日志/进入 Shell 操作绑定已连接的 SSH session，旧异步结果不能覆盖切换后的目标。
- 只允许对状态为 running 的容器打开 Shell。新建 Motty 终端标签页并复用当前已认证 SSH 连接；在新标签页的 PTY 中运行选择好的 `docker exec -it`，原终端保持不变。容器中依次尝试可用的 POSIX Shell（例如 `sh`、`bash`、`ash`）；没有可启动 Shell 时清楚地提示用户，而不是进入后隐式重复发送命令。成功启动 Shell 后，用户输入的命令和控制字节交由现有终端传输链路处理；非零退出码不触发 Shell 切换。关闭新标签页只关闭其 PTY，不停止容器。
- 已知 Windows OpenSSH 主机不提供尚未验证的 POSIX 容器终端；其它 SSH 主机仅在 `docker exec` 对固定 POSIX Shell 的启动能力探测成功后才开放进入。未实现 `cmd.exe` / PowerShell 容器终端，也不凭宿主系统猜测容器 OS；跨引擎 Windows 容器若自带 POSIX Shell 的情形未作原生 Windows 命令兼容承诺。
- 不对用户保存的主机执行 stop/start/restart/rm、镜像清理或权限修改。只读列表和可用时的交互 Shell 验证不等同于发布验收。

## 兼容与验收

- 复现用户截图错误：`Template parsing error ... <.State>: can't evaluate field State in type *formatter.containerContext`；旧模板回退成功显示容器名称、镜像与推导状态。现代模板不多请求；普通 daemon 错误不触发旧模板。
- 全部列表为空与解析/执行失败区分；旧版状态字符串包括 Up、Exited、Created、Restarting、Paused、Dead；无法识别时用 unknown，不把 unknown 宣称为 running。
- 进入容器前先用 `docker inspect` 将列表中的短 ID 解析成经过校验的完整 ID，再对固定 Shell 候选做安全探测；不能解析完整 ID 或不能执行候选时提示原因。只把明确指向候选可执行文件缺失的错误作为“试下一个 Shell”，工作目录失效等 runtime 错误不能掩盖为容器没装 Shell。Shell 使用同一个 SSH 主机和完整容器 ID，不允许容器名或 Shell 字符串混入远端命令语法。
- 纯 Go、Viva 界面和 SSH PTY 测试覆盖；本机编译与安装后，用现有保存的主机只读核对。最低 Docker 版本只在真版本矩阵测试通过后承诺。
- 镜像 `.CreatedSince`、卷/网络 `.Scope`、进程 `ps -eo` 等分别用旧/新假输出和实际退出状态测试；旧版缺失可选列显示明确的中性占位，而非伪造值；Compose 无可验证的兼容命令则正确报不支持。

## 非范围

- 新工具栏图标、新 WebView/前端框架、Docker Engine API、Podman 自动发现。
- 假定任何正在运行的容器必有 Shell；为容器安装 Shell 或改变容器状态。
- 对已保存生产主机运行 Docker 写操作，或为了通过测试改变主机认证/权限。

## 本地验证记录（2026-10-27）

- `gofmt`、`go vet ./...`、开发标签的 workbench vet、`go test ./... -count=1`、`go test -race ./... -count=1` 均在 Go + 当前 Viva 源码的隔离工作区通过。
- `tools/e2e/remote_tools_legacy_e2e.txt` 使用本机 loopback SSH fixture 和假 Docker/ps，实际走了现有“远程工具”六个标签：旧容器列表、镜像、卷、网络、Compose 不支持提示、旧 ps 进程列表，以及独立容器标签页启动。无头布局报 `0 problems`。fixture 中的容器终端启动后立即退出，**不能**代表真实容器持续交互已验收。
- 本机并行安装了独立的开发版 `.app`，保持原安装不变；包内二进制一致性、代码签名校验及原生窗口启动已检查。使用只包含此任务快照的保存主机配置，用户截图中的旧版 Docker 主机经修复后能显示两条运行容器及镜像，原 `.State` 模板错误不再出现；读到了卷、网络列表。随后关闭开发版并清理临时敏感配置快照。没有对真实容器运行 `exec`、启停、删除或清理。
- 真实主机 Docker CLI/daemon 的精确版本尚未记录；仅可断言此主机不识别 `.State`，**不能**据此承诺支持 Docker 1.12 或其它明确最低版本。镜像、Compose、旧 ps 兼容分支经 fake CLI/headless 验证；真实 Windows/Linux 其它版本组合、无 Shell 镜像及跨平台发行包未完成验收。
- 代码评审追加验证：独立容器终端不保存为可自动重连的普通主机标签；打开另一台主机时旧异步结果不能覆盖新主机列表。Windows OpenSSH 主机不提供 POSIX Shell 入口；Windows 容器在 Linux SSH 主机上的原生命令环境未实机验证，不能据 POSIX 启动探测宣传支持 Windows 容器。
