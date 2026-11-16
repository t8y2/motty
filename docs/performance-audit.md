# Motty 性能排查报告

- 日期：2026-10-23
- 范围：本仓库全部 Go 包，重点是终端、渲染、SSH/SFTP、持久化、启动与空闲行为。
- 方法：阅读热路径代码 + 运行仓库已有 benchmark + 对新增疑点写一次性探针（探针已删除，
  未进仓库）+ `pprof` CPU/alloc profile + 无头进程启动/空闲实测。
- 审计时未改动产品代码；2026-10-23 已落地 H1、W4、W2、W3、S2、S1、T1、T2、W1 的增量保存与 A1 的 SSE 复用，见 §10。

> **状态图例（2026-10-23）**：~~删除线~~ = 已完成并提交，证据见 §10；未划掉 = 待办/后续。已完成：H1、S1、S2、W1、W2、W3、W4、T1、T2（批量 ASCII；环形屏未采用）、A1、图片可见行定位。后续：M1 回滚区/图片预算、R1、V1。

## 0. 结论摘要

Motty 的基础性能是健康的：空闲 CPU 实测低于 0.01%，常驻约 50 MB，冷启动约 80–103 ms（无头），
稳定帧的窗口绘制约 30–100 µs 且 0 alloc。真正值得做的优化集中在少数几个热点上，按收益排序：

| 优先级 | 编号 | 位置 | 问题 | 实测/推算影响 | 归属 |
|---|---|---|---|---|---|
| ~~P0~~ | ~~H1~~ | ~~`highlight` / 终端行缓存~~ | ~~每行跑 4 条正则；高亮占终端刷新帧 CPU 的 **55%**~~ | ~~单行 22.7 µs → 字面量匹配原型 **0.5–0.95 µs**（13–27×）；预计整帧约 2×~~ | ~~Motty~~ |
| ~~P0~~ | ~~S1~~ | ~~`sftpclient` 传输~~ | ~~32 KiB 读/写各等一次 RTT；未用并发读写~~ | ~~已落地流水线：下载 `WriteTo`、上传 16 路、续传校验 8 路~~ | ~~Motty~~ |
| ~~P1~~ | ~~S2~~ | ~~`sftpclient.describe`~~ | ~~每个符号链接串行 2 次 RTT，最多 200 个，结果全齐才返回列表~~ | ~~已落地渐进式：首屏 **4 次往返**（实测 9 ms @2 ms RTT）；链接在后台补齐~~ | ~~Motty~~ |
| ~~P1~~ | ~~W1~~ | ~~`persist_records.splitState`~~ | ~~每次保存把整个状态 JSON 编码→解码→逐条再编码~~ | ~~已落地增量保存：单设置 1.08 ms / 301 KB（完整 2.99 ms / 900 KB）；多字段也走增量~~ | ~~Motty~~ |
| ~~P1~~ | ~~W2~~ | ~~`terminal_find.refreshIfChanged`~~ | ~~find 打开时每次终端版本变化整段重扫~~ | ~~5000 行回滚区 **1.62 ms/次**；流式输出时可达 60 次/秒~~ | ~~Motty~~ |
| ~~P2~~ | ~~W3~~ | ~~`sftp.listLocal` / `prepareSFTPRows`~~ | ~~本地目录枚举、排序、行格式化全在窗口线程~~ | ~~已落地：本地只同步读前 513 项，其余枚举/排序/行准备离线程；远程大列表（含链接）也离线程~~ | ~~Motty~~ |
| ~~P2~~ | ~~W4~~ | ~~`persist.restoreShared` / `uniqueCatalogID`~~ | ~~主机目录恢复时逐台线性查重，O(n²)~~ | ~~5000 主机启动 **73.8 ms / 30.7 MB**；`uniqueCatalogID` 占 profile **32%**~~ | ~~Motty~~ |
| ~~P2~~ | ~~T1~~ | ~~`pump`~~ | ~~一次 Wake 最多解析 1 MiB 输出~~ | ~~已落地：每 Wake 最多 128 KiB，余量下一帧；1 MiB 不再一次停顿~~ | ~~Motty~~ |
| ~~P2~~ | ~~T2~~ | ~~`term` 滚动/打印~~ | ~~每次滚屏复制整屏指针；ASCII 逐字符写~~ | ~~已落地批量 ASCII（210 → **305 MB/s**）；环形屏原型无可测收益，未采用~~ | ~~Motty~~ |
| P3 | M1 | 内存预算 | 回滚区按行数；列表缓存按目录数 | 已给 SFTP 列表缓存加 **32 MB** 字节上限；回滚区/图片上限未变 | Motty |
| ~~P3~~ | ~~A1~~ | ~~AI 流解析~~ | ~~每 MB 流产生约 12 MB 垃圾~~ | ~~SSE 解析已 2 allocs/MiB；OpenAI 流 7.0 MB/166k allocs per MiB（复用 chunk）~~ | ~~Motty~~ |
| P3 | R1 | RDP 解码 | 1080p planar/nscodec 解码约 3.6 ms/帧 | 1080p 0.94/1.56 ms；4K planar 7.4 ms、NSCodec 4.5 ms、RLE 9.4 ms（8 路）；60 fps 预算内 | Motty |
| P3 | V1 | Viva 图标重放 | 每次重绘重放三角面 | hover 帧 profile 的 **18%**；1 µs/图标 | Viva |

> “实测”指本机 benchmark/进程测量；“推算”指由代码路径与常量推导、未在真实广域网上验证。
> 表中所有数字都是 Apple M2 Pro、Go 1.26.5 上的单次或三次运行结果，详见 §2 与 §7。

**一句话结论**：终端高亮是唯一同时满足“热点占比高、修复局部、收益 10× 以上”的 P0；
SFTP 的往返次数和传输并发是广域网体验的最大短板；保存与 find 重扫是窗口线程上的
1–2 ms 级停顿。其余属于“可做但不急”的优化。

## 1. 环境与方法

- 机器：Apple M2 Pro（10 核），macOS，Go 1.26.5 darwin/arm64。
- 构建：`go build -tags vivahead -o /tmp/motty-head ./cmd/motty`；benchmark 使用
  `-tags vivahead`，临时 `GOCACHE` 避免沙箱对 `~/Library/Caches/go-build` 的限制。
- 基准：`go test -tags vivahead ./... -run '^$' -bench . -benchmem -benchtime 200ms -count 1`
  （完整结果见 §7）。
- profile：对 `BenchmarkPaintTerminalFlood`、`BenchmarkWholeWindowPaintThroughTheDriver`、
  `BenchmarkSaveNothingShared`、`BenchmarkWritePlainText/CJK/Colored` 采 CPU 与 alloc profile。
- 启动/空闲：脚本启动无头二进制（每次全新 `HOME`），用 `proc_pid_rusage`（`libproc`）
  采样 RSS/CPU；`SIGQUIT` 取 goroutine dump；`GODEBUG=inittrace=1` 看包初始化。
- 探针（一次性，已删除）：
  - 字面量高亮匹配器 vs 现有正则，5 条真实行 + 20 万条生成行做逐字节等价检查；
  - 5000×120 回滚区内存增量；
  - 2 万条本地目录项排序（`strings.ToLower` 比较器 vs 预计算小写）；
  - 用伪造 `sftpclient.Remote`（每次往返 sleep 2 ms）统计一次带符号链接的列目录
    往返次数与首屏耗时。

**测量边界**：无头构建不经过 CoreText 排版与 Metal 呈现，所以窗口绘制数字不包含字形
栅格化、GPU 上传与 vsync；真实窗口的数字会不同。本文所有结论都不依赖真实窗口；
需要真机确认的地方在 §8 标出。

## 2. 基线数据

### 2.1 进程

| 指标 | 结果 | 说明 |
|---|---|---|
| 冷启动到 `ok ready` | 80 / 96 / 103 ms（3 次） | 含进程 exec；每次全新 HOME |
| 常驻内存（ready 后稳定） | 49–50 MB | RSS；无窗口、无会话 |
| 空闲 CPU（ready 后 5.6 s） | < 0.01% | CPU 时间增量低于 1 ms |
| 空闲 goroutine | 19 | 10 个 GC worker、runtime 后台任务、1 个等 stdin 的 `select`；无 timer/ticker/netpoll |
| 可执行文件 | 30 MB（未剥离）/ 21 MB（`-s -w`） | 图标数据包约占一部分 |
| Go 包初始化 | 34 ms | `modernc.org/libc/.../netdb` 3.3 ms / 3.6 MB；icons 2.2 ms；workbench 2.3 ms |
| Motty 状态初始化 | 0.34 ms（空 DB）/ 1.19 ms（新建 DB） | `workbench.New`；不含 Viva/窗口 |
| 主机目录对启动的影响 | 300 台 2.7 ms；5000 台 **73.8 ms** | 见 §3.6.1 的 O(n²) 查重 |

启动的 80–103 ms 里 Motty 自己的状态装载在空/常规目录下只占 0.3–2.7 ms；其余主要是
Go init、Viva/平台初始化与进程启动。**常规规模下启动不是瓶颈**；数千主机的目录会让
启动多花几十毫秒，见 §3.6.1。

### 2.2 绘制

| Benchmark | 结果 | alloc |
|---|---|---|
| WholeWindowPaint hosts-10 / hosts-1000 | 109 / 122 µs | 0 |
| WholeWindowPaint terminal-two-panes-files | 72 µs | 1 |
| WholeWindowPaintThroughTheDriver（hover 往返） | 209–214 µs | 4 / 2 976 B |
| KnownHostsPageThroughTheDriver | 101–104 µs | 0 |
| PaintTerminalUnchanged | 40.5 µs | 0 |
| PaintTerminalFlood（每帧 60 行新输出） | **1.10–1.13 ms** | 85–103 / 44–58 KB |
| PaintSFTPPage5000 | 94 µs | 0 |
| PaintAIPanel 500 条消息 repaint / resize | 78 µs / 349 µs | 500 条 resize：801 / 42 KB |
| KeystrokeToEcho（真实 painter/router/文本输入路径） | 202 µs | 4 / 151 B |

结论：**稳定帧很便宜，变化帧才是成本**；鼠标 hover 一次约 0.21 ms，按键回显约 0.20 ms，
都在 60 Hz 预算内。终端 flood 帧的 1.1 ms 是最高的一项，下一节说明它花在哪。

### 2.3 终端解析

| Benchmark | 吞吐（CPU） | 备注 |
|---|---|---|
| WritePlainText | 210–219 MB/s | 102 KB/op，1 alloc |
| WriteColoredText | 158 MB/s | SGR 每几个字节一个 |
| WriteCJKText | 115 MB/s | 宽字符 + 组合标记路径 |
| WriteFullScreenRedraw | 186 MB/s | 40 行逐格重绘，0 alloc |

CPU profile（三个写 benchmark 合计）：

| 函数 | flat |
|---|---|
| `printASCII` | 27.5% |
| `Write` 主循环 | 16.6% |
| `feed` 状态机 | 8.6% |
| `recycle` | 8.5% |
| `pushHistory` | 6.4% |
| `rotate`（滚屏复制） | 21.9%（含 `scrollUp`/`index`） |
| `memmove` | 5.4% |
| `Width` / `isWide` | 5.0% |

5000 行 × 120 列回滚区：**堆增量 +9.0 MB，累计分配 9.7 MB**（`Cell` 16 B + 行结构）。

## 3. 发现详情

### ~~3.1 H1（P0）终端高亮的正则成本~~

**现象**

终端每准备一行（新建行、内容变化、从回滚区滚入视野、调整窗口、换主题）都会跑
`rowCache.markCells`，把行文本依次交给 4 条内置正则：

- 红：`\b(error|errors|err|fail|...)\b` 共 18 个分支；
- 黄：`\b(warn|warning|...)\b`；
- 绿：`\b(ok|success|...|active \(running\))\b`；
- 青：IPv4 的 `\b\d{1,3}(\.\d{1,3}){3}\b`。

见 [`internal/highlight/highlight.go:158`](../internal/highlight/highlight.go#L158)、
[`internal/highlight/highlight.go:179`](../internal/highlight/highlight.go#L179)、
[`internal/workbench/termview.go:304`](../internal/workbench/termview.go#L304)。

**证据**

- 仓库已有 benchmark `BenchmarkPrepareALogLine` 就是量这个：无规则 **2.08 µs/行**，
  内置规则 **22.7 µs/行**（644 B、5 allocs）——
  见 [`internal/workbench/terminal_tools_test.go:483`](../internal/workbench/terminal_tools_test.go#L483)。
  也就是说，这个（含命中的）benchmark 行的“准备”成本里约 **91%** 是高亮；
  对不含命中的普通行，高亮占比也在 80% 左右。
- `BenchmarkPaintTerminalFlood` 的 CPU profile 中，`markCells` 累计 **55.5%**，
  `regexp.tryBacktrack`/`bitState`/`SimpleFold` 等正则内部函数合计约 46%。
- 终端滚动/输出时每帧通常有几十行是“新行”，因此 flood 帧的 1.1 ms 中约一半是正则；
  在回滚区里向上滚动一屏（40 行新可见）约需 40×11–19 µs ≈ **0.4–0.8 ms**，而内容未变的
  帧只要 40 µs。

**一次性原型（未提交）**

把三条词规则改成“小写化一次 + 字面量 `bytes.Index` + 精确 `\b` 边界检查”，
IPv4 改成专用四段数字扫描，保留优先级（红→黄→绿→青）和“已被前规则标记的字节不覆盖”：

| 行 | 现有正则 | 字面量原型 | 加速 |
|---|---|---|---|
| sshd Failed password + IP | 19.7 µs | 0.95 µs | 21× |
| 纯文本日志 | 10.9 µs | 0.75 µs | 15× |
| GET /api … 200 12ms | 13.2 µs | 0.61 µs | 22× |
| build OK on 10.0.0.12 | 13.4 µs | 0.49 µs | 27× |
| 一条含多种命中的行 | 10.2 µs | 0.83 µs | 12× |

在 5 条真实行 + 20 万条随机组合的生成行（大小写、标点、重叠加词、IPv4 边界、
非 ASCII）上，标记结果与现有实现**逐字节一致**。

**落地结果（2026-10-23）**

已按下面的“字面量规则”方案实现，并保留正则作为非 ASCII/超长行的回退：

| 指标 | 落地前 | 落地后 |
|---|---|---|
| `BenchmarkMarkABuiltinLine`（命中行） | 19.7 µs | **0.90 µs**（22×），0 allocs |
| 新增 `BenchmarkMarkABuiltinPlainLine`（普通行） | — | **0.54 µs**，0 allocs |
| `BenchmarkPrepareALogLine/builtin_rules` | 22.7 µs | **3.75 µs** |
| 高亮在行准备中的增量 | 20.6 µs | **1.54 µs**（13×） |
| `BenchmarkPaintTerminalFlood` | 1.10–1.13 ms | **0.328 ms**（3.4×） |
| flood 帧分配 | 85–103 allocs / 44–58 KB | 57 allocs / 22 KB |

实现要点：纯 ASCII、≤512 字节的行使用内置字面量匹配；任何非 ASCII 字节或更长的行
仍走原来的正则，所以 Unicode 大小写折叠语义不变；用户自定义规则仍走正则并保持优先级。
等价性由 5 条手写行、2 000 条生成行、5 000 条 IPv4 随机串和 10 秒 fuzz（87 k execs）
共同验证，另有 0-alloc 测试守护快路径。详见 §10。

**审计时的两步方案（第 2 步已落地）**

保留在这里，供以后修改自定义规则/内置规则时参考：

1. **保守预过滤（低风险）**：给每条内置规则附一组“必须出现的字面量”（大小写不敏感），
   在跑正则前用一次性小写副本做 `bytes.Contains`；没有字面量就跳过该正则。
   普通行（大多数）可从 11–19 µs 降到约 0.4–0.6 µs；命中的行只跑命中的规则。
   正则语义完全不变，适合先落地。
2. **字面量规则（推荐终态）**：在 `highlight.Rule` 上增加“整词字面量”种类，内置规则用
   字面量匹配器 + 专用 IPv4 扫描，用户自定义的正则规则仍走 `regexp`。
   实测单行 0.54–0.90 µs，flood 帧 0.33 ms（见上面的落地结果）。

**必须注意的细节**

- `\b` 是 word/non-word 的“状态变化”，不是“旁边不是字母”。特别地，内置绿规则里的
  `active \(running\)` 以 `)` 结尾，而 `\b` 要求 `)` 后紧跟 word 字符，所以这个分支
  **实际上永远匹配不到**（“active (running) ”不会命中，“active (running)x”才会）。
  这是现有行为，不是本次要顺手改掉的 bug；字面量匹配器必须复刻它，或者单独立项修
  规则并加测试，二者不要混在一个性能改动里。
- 用户自定义规则排在内置规则之前，且“已标记的字节不覆盖”。字面量路径必须保留这个优先级。
- 现有 `Set.Mark` 的 API 可以不变；在 `Compile` 时把规则分类即可。需要补：
  - `BenchmarkPrepareALogLine` 增加 no-match 行（现在只有命中行），
  - 一个字面量/正则等价性测试（生成的用例集要覆盖上面的边界），
  - 一个 alloc/耗时门槛，避免以后回退。

### ~~3.2 S1（P0）SFTP 单文件吞吐受限于 32 KiB/RTT~~

**代码路径**

- 下载：`downloadFile` → [`worker.copy`](../internal/sftpclient/worker.go#L1441)，
  每次 `Read` 32 KiB、再 `Write` 32 KiB；上传同理，见
  [`internal/sftpclient/worker.go:1096`](../internal/sftpclient/worker.go#L1096)、
  [`internal/sftpclient/worker.go:1356`](../internal/sftpclient/worker.go#L1356)。
- `sftp.NewClient` 没有传任何选项，
  见 [`internal/sftpclient/client.go:54`](../internal/sftpclient/client.go#L54)；
  默认 `maxPacket = 32 KiB`，`File.Read` 在请求不超过一个 packet 时**一次请求一次往返**。
- pkg/sftp 自带 `File.WriteTo`（默认开启并发读）与 `File.ReadFrom`/`UseConcurrentWrites`
  （并发写），Motty 没有用；动态类型本来是 `*sftp.File`，但被 `io.ReadCloser` 包装后
  手写 copy 绕开了 `io.Copy` 的 `WriterTo`/`ReaderFrom` 优化。

**推算**

单文件吞吐 ≈ 32 KiB / RTT（服务端读写时间另计）：

| RTT | 单文件 | 默认 2 个并行文件合计 |
|---|---|---|
| 1 ms | 32 MB/s | 64 MB/s |
| 5 ms | 6.4 MB/s | 12.8 MB/s |
| 20 ms | 1.6 MB/s | 3.2 MB/s |
| 50 ms | 0.65 MB/s | 1.3 MB/s |
| 100 ms | 0.33 MB/s | 0.65 MB/s |

局域网上限由服务器和 SSH 窗口决定，问题不明显；Motty 是 SSH 客户端，广域网才是主场景，
这里的上限会直接变成用户感知的“传大文件很慢”。

**落地结果（2026-10-23，部分）**

- 下载：`downloadFile`/`legacyDownload` 在源实现 `io.WriterTo` 时调用 `File.WriteTo`，
  用 `copyProgress` 包装本地 writer 处理暂停、取消和进度；pkg/sftp 默认并发读最多
  64 个 32 KiB 请求，不再是一个 RTT 一个块。若 `WriteTo` 在写出任何字节前失败
  （例如服务器拒绝对打开的文件做 stat），回退到原来的 32 KiB 顺序读。
- 上传：`uploadFile`/`legacyUpload` 在目标实现 `ReadFromWithConcurrency` 时用
  **16 路**并发写，`copyReadProgress` 在读取侧处理暂停/取消和进度；同样有顺序回退。
- 测试：新的 `download_copy_test.go`/`upload_copy_test.go` 覆盖使用、顺序回退和
  “先失败再回退”；`TestLargeFilesTransferThroughThePipelinedPath` 用 512 KiB
  真实 SFTP 往返验证内容。
- 续传校验：`verifyPrefix` 在两端都支持 `io.ReaderAt`/`io.Seeker` 时用 **8 路**、
  64 KiB 一块并发比较，结束后把两端 Seek 到前缀末尾；否则走原来的 32 KiB 顺序路径。
  `verifyChunk`/`verifyWorkers` 在 `worker.go`，另有 tracking ReaderAt 测试证明读取确实
  重叠，以及 mismatch/fallback/cancel 用例。
- 未完成：并发读写的高 RTT 真机吞吐还没有实测。

**审计时的建议**

- 下载：用 `File.WriteTo`（或 `io.Copy`，让 `WriterTo` 生效）配合一个包装 writer：
  每写一块检查暂停/取消、更新进度；并发度默认 8–16（pkg/sftp 默认上限 64），
  对“读取一次就删除”的服务器保留关闭并发的选项。
- 上传：`sftp.UseConcurrentWrites(true)` + `ReadFrom`（或 `ReadFromWithConcurrency`）；
  按 pkg/sftp 的文档，出错后要 `Truncate` 到已写长度，避免空洞。
- 并发读/写会改变暂停粒度（从 32 KiB 变为一个并发窗口），需要把暂停语义定义为
  “不再提交新请求，等在途请求完成”；取消则关闭文件句柄。
- 恢复（resume）路径还有第二笔账：
  [`verifyPrefix`](../internal/sftpclient/worker.go#L1481) 在续传前把已有前缀
  从远端和本地各完整读一遍（32 KiB 一对，串行）。10 GB 文件续传 9 GB 时，校验本身
  要再走 9 GB，且是 32 KiB/RTT。至少应并发校验，或改用“大小 + mtime + 分块哈希”
  的标准方案，避免 O(已传字节) 的前置等待。
- 验证需要真实高 RTT 环境：`e2esshd` 是回环，测不出这个问题；用 `tc netem`
  或在测试夹具前加一个延迟代理，分别测 5/50/100 ms 下 100 MB 单文件的耗时。

### ~~3.3 S2（P1）SFTP 列目录被符号链接解析拖住~~

**代码路径**

[`Worker.list`](../internal/sftpclient/worker.go#L872) 先 `ReadDir`，再同步调用
[`describe`](../internal/sftpclient/worker.go#L894)；后者对每个符号链接依次
`ReadLink` 再 `Stat`，最多 `maxLinkLookups = 200` 个
（[`worker.go:137`](../internal/sftpclient/worker.go#L137)），全部完成后才把 entries
返回给 UI。`ReadLink` 和 `Stat` 各是一次 SFTP 请求/响应，串行。

**证据（模拟）**

伪造 `Remote`，每次往返 2 ms：

| 链接数 | 往返次数 | 首屏耗时 |
|---|---|---|
| 0 | 4 | 10 ms |
| 50 | 104 | 259 ms |
| 200 | 404 | 1 003 ms |

按同一往返次数外推：40 ms RTT 下 200 个链接要 **16.2 s** 才显示第一行列表；
常见 Linux 的 `/usr/bin`、`/etc/alternatives` 很容易超过 200 个链接。

**落地结果（2026-10-23）**

已按下面的“先出列表，链接信息后补”实现：`list` 在 `ReadDir` 和 owner/group 名字之后
立即返回 entries；若其中有符号链接，`Worker.run` 在**首发结果发布之后**另起一个
goroutine 解析链接（仍受 `maxLinkLookups` 限制），完成后发布 `FollowUp` 结果。
文件面板收到 FollowUp 时只更新行，不改变选择或滚动位置；已读目录的缓存也同步更新。
取消/关闭时 generation 失效，跟进结果被丢弃，`linksGroup.Wait` 保证结果 channel
在所有跟进发布之后才关闭。

用伪造 Remote（每次往返 2 ms、200 个链接）实测：

| 指标 | 落地前 | 落地后 |
|---|---|---|
| 首屏 | 1 003 ms / 404 次往返 | **9 ms / 4 次往返** |
| 链接解析 | 阻塞首屏 | 后台 **912 ms** 完成，随后原地补全 |

按 40 ms RTT 外推，首屏从约 **16 s** 降到约 **0.16 s**；链接细节仍在后台补齐。
测试覆盖首发先到、顺序、FollowUp 字段、无链接不产生 FollowUp，以及文件面板在
40 ms 链接延迟下先显示再补全。

**审计时的建议**

标准做法是“先出列表，链接信息后补”：

1. `ReadDir` 后立即返回 entries（`LinkTarget`/`LinkDir` 留空），列表马上可见；
2. 只为**当前可见行**解析链接，或按小并发（如 8）在后台解析并逐批 `updateSFTPRows`；
3. 解析结果按 `(directory, name, mtime, size)` 缓存，重新进入同一目录不重复请求；
4. 保留 `maxLinkLookups` 作为上限，但把它从“阻塞首屏”改成“后台补全的预算”。

UI 已经支持 `finishSFTPPrefetch`/`updateSFTPRows` 的增量更新，改成渐进式不需要新页面。

### ~~3.4 W1（P1）保存每次重编码整个状态~~

**现象与证据**

`state.save()` 在窗口线程、几乎每次用户改动后调用（94 个调用点），
见 [`internal/workbench/persist.go:436`](../internal/workbench/persist.go#L436)。
它调用 [`splitState`](../internal/workbench/persist_records.go#L238)：
先把整个 `savedState` `json.Marshal`，再 `json.Unmarshal` 成
`map[string]json.RawMessage`，然后对每个字段/每条记录重新 `Marshal`/`Unmarshal`，
最后才做 diff 并提交真正变化的记录。

300 个主机时实测：

| 场景 | 耗时 | 分配 | allocs |
|---|---|---|---|
| 没有任何变化 | 1.35–1.38 ms | 697–708 KB | 9 132 |
| 改一个主机标签 | 2.18–2.19 ms | 874–887 KB | 11 329 |
| 仅 `snapshot()` | 20.9 µs | 82.7 KB | 2 |
| 仅 `splitState()` | **1.31 ms** | **605 KB** | **9 148** |

结论很明确：95% 的时间在 `splitState` 的 JSON 往返，不在数据库提交，也不在快照拷贝
（`BenchmarkSave*` 在 [`persist_records_test.go:245`](../internal/workbench/persist_records_test.go#L245)）。
主机数增长时成本近似线性。

**落地结果（2026-10-23，部分）**

`save()` 现在先取快照；若与上次成功保存的快照 `reflect.DeepEqual`，直接返回。
300 主机、无任何变化的保存从 **1.35 ms / 708 KB / 9.1k allocs** 降到
**约 106 µs / 84 KB / 4 allocs**；有变化的保存仍走完整路径（多约 0.1 ms 的比较）。
深比较前测试了快照不共享可变状态（`Deck.DTO()` 等均为新切片）。

只有**一个**顶层字段变化时走 `writeOneField`：其他 kind 的记录和本机值直接从 base
复用，只编码变化的列表或设置（保持 id、`ord`、重复后缀和删除语义）。
`changedSavedFields` 用字段级比较列出所有变化；`writeChangedFields` 在一次提交里
处理**多个**变化的列表、共享设置和本机值。`host_groups` 也走增量：记录里去掉
`collapsed`，折叠名单写进本机 `collapsed_groups`；`ai_providers` 的 nil（尚未读取）
保持 base 记录，非 nil 按列表处理。只有状态版本字段回退完整 `splitState`。
`TestChangedSavedFieldsRecognizesOneFieldChanges`、`TestSavingOneSettingUsesTheIncrementalPath`、
`TestSavingOneDeviceValueUsesTheLocalShortcut` 与 `TestSavingSeveralFieldsAtOnceKeepsAll`
守护边界。

单个**本机值**（窗口、语言、面板宽度等）变化时更新 `base.local` 里的对应键，然后
提交本机值，不碰共享记录。

300 主机、当前负载下：无变化 110 µs / 84 KB；单个设置 1.08 ms / 301 KB
（完整路径 2.99 ms / 900 KB）；单个本机值 0.63 ms / 90 KB；单个主机
2.46 ms / 792 KB。主机/设置路径仍受 SQLite 提交和列表编码成本影响。

**审计时的建议**

1. **去掉“整状态 JSON → map → 再编码”**：直接按实体/设置逐条构造
   `configstore.Entry`，用缓存的每记录编码或结构体比较来跳过未变记录。
   变更检测可以用每个记录的 dirty generation，而不是序列化后比较字节。
2. 快照与编码可以移出窗口线程（保存的 diff 语义不变，提交仍回窗口线程），
   或至少把连续改动合并（例如 50 ms 去抖/下一帧提交）；现有 `sync` 已经有 5 s 去抖，
   但 `save` 本身没有。
3. 目标：300 主机下 **< 100 µs、< 50 KB**；改一个主机不重编码其他 299 个。
   用现有两个 benchmark 加 alloc 门槛即可守护。
4. 这条改动触碰跨设备 stamp/合并语义，属于数据结构层改动，建议单独一个 PR，
   用现有 `configstore` 与 sync 测试兜底。

### ~~3.5 W2（P1）find 打开时每次输出都整段重扫~~

**代码路径**

[`terminalFind.refreshIfChanged`](../internal/workbench/terminal_find.go#L85) 在
**每次** `screen.Version()` 变化时调用 `Search`
（[`internal/term/search.go:22`](../internal/term/search.go#L22)），而 `Wake` 对每个
session 都调用它，见 [`internal/workbench/connection.go:555`](../internal/workbench/connection.go#L555)。
`Search` 实测 5000 行回滚区 **1.62 ms**（2 allocs）。

**影响**

- find bar 打开且输出持续（`tail -f`、构建日志）时，每次 Wake 都重扫；
  Viva 的无效区合并到显示刷新后，上限约 60 次/秒 → 约 **97 ms/s（≈10% 单核）**，
  5000 行回滚区越大越糟。
- 分屏里多个 pane 各开 find 时按 pane 数放大；不可见的 pane 也会被 `pump` 扫到。
- 每次重扫还会重建最多 1000 个 `SearchMatch`，虽然复用切片，但会持续占用窗口线程。

**落地结果（2026-10-23）**

`refreshIfChanged` 现在接收 workbench 时钟：距上次自动扫描不足
`terminalFindRefreshInterval`（100 ms）时只置 `pending`，不再重扫；终端的
`paintGrid` 用 `canvas.RequestFrameIn` 申请补齐帧，下一次 `Wake` 在间隔到点后扫描并
清除 pending。打开、配置、resize、修改 scrollback 等“用户刚做了事”的路径传 0，
仍然立即刷新。这样流式输出时每次扫描从“每个版本一次”降到最多 10 次/秒
（5000 行回滚区约 97 ms/s → ≤16 ms/s，后者为按 1.62 ms/次推算），且输出停止后
仍有一次补齐扫描，不会停在旧结果上。测试覆盖了节流、pending、补齐和申请帧。

**审计时的建议**

- 最小改动：`refreshIfChanged` 加 100–150 ms 去抖，只在用户输入/回车时立即刷新；
  输出流动时最多每 150 ms 重扫一次，find 计数暂时落后可接受。
- 更好的做法：保留每行的增量扫描游标，只扫新增/变化的行；或把扫描放到后台
  goroutine，带 version 校验回到窗口线程（与现有 `aiPost` 模式一致）。
- 不可见 pane 不应刷新（或在真正显示时刷新）。
- 验证：加一个“find 打开 + 每秒 1 MB 输出”的 benchmark，断言窗口线程每帧不跑
  `Search`。

### ~~3.6 W3（P2）窗口线程上的本地目录处理~~

**证据**

- `prepareSFTPRows`（[`sftp.go:1061`](../internal/workbench/sftp.go#L1061)）对 5000 个
  远端条目：**1.08 ms、172 KB、15 000 allocs**（一次性 benchmark）。20 000 条约 4.3 ms。
  该函数在 `showSFTPListing`/`updateSFTPRows` 里、窗口线程上执行。
- 本地面板 [`listLocal`](../internal/workbench/sftp_local.go#L69) 更重：`os.ReadDir` 后
  对每个 entry 调 `Info()`（2 万次 stat），符号链接再各 `Readlink`+`Stat`，
  然后用 `strings.ToLower` 在比较器里排序
  （[`sftp_local.go:102`](../internal/workbench/sftp_local.go#L102)）。
  2 万条随机顺序排序实测 **5.8 ms**（预计算小写后 2.5 ms；比较器每次比较分配 2 个字符串），
  加上枚举/行格式化，打开一个 2 万文件的目录会在窗口线程上占用十几毫秒。
- 这两处都有硬上限（`maxLocalEntries = 20000`、`maxSFTPListingEntries = 20000`），
  所以不会无界，但大目录的首帧会掉。

**落地结果（2026-10-23）**

本地面板的排序已改为“先对每个名字做一次 `ToLower`，再按索引排序”，不再在比较器里
每次比较分配两个字符串。2 万条随机顺序的排序从约 **5.8 ms** 降到约 **2.5 ms**，
比较期间的临时分配基本消失。

超过 `localListingInlineEntries = 512` 项的目录现在把 `Info()`、符号链接解析、排序和
`prepareSFTPRows` 全部移到 goroutine，窗口线程只做 `os.ReadDir` 和最终应用；
`localSeq` 丢弃被更新的列表超过的结果，切换回服务器也会使其失效。

远程目录同理：列表结果超过 `sftpPrepInlineEntries = 512` 项时，`prepareSFTPRows`
在 goroutine 里准备，`remoteSeq` 丢弃被新列表/缓存列表超过的结果。测试覆盖 576 项
本地目录、600 项远程目录的后台准备，以及“旧列表晚到不覆盖新列表”。

**审计时的建议**

- 本地枚举/排序/行准备移到 worker goroutine（SFTP 远端已经是 worker 模型），
  或者至少把 2 万次 `Info` 与排序分帧做；
- 排序前一次性计算 folded name（或给 `Entry` 加内部字段），不要在每个比较器里 `ToLower`；
- `prepareSFTPRows` 只对可见窗口 + 预取窗口格式化，其余按需；或缓存 `cells` 字符串；
- 给本地/远端列表各加一个“2 万条”的 benchmark（现在只有 `PaintSFTPPage5000`，
  不覆盖行准备）。

### ~~3.6.1 W4（P2）主机目录恢复的 O(n²) 查重~~

**现象**

`restoreShared` 逐台恢复主机时，对每台调用
[`uniqueCatalogID(host.CatalogID, host, state.hosts)`](../internal/workbench/persist.go#L586)，
后者对已恢复的主机列表做线性扫描（[`host_catalog.go:155`](../internal/workbench/host_catalog.go#L155)）。
5000 台主机就是约 1250 万次字符串比较；`saved.Hosts` 本身已经带有唯一 ID，
这里只是防御重复。

**证据**

| 主机数 | `New`（已存 DB） | 分配 | allocs |
|---|---|---|---|
| 300 | 2.74 ms | 1.7 MB | 11.9 k |
| 5000 | **73.8 ms** | **30.7 MB** | 176.5 k |

5000 主机的 CPU profile：`uniqueCatalogID` 累计 **32.5%**（其中 `runtime.memequal`
flat 11.6%），`runtime.madvise` 36%（大量分配的 GC 归还）；
`joinState`/`recordParses`/`restoreShared` 是其余大头。

**落地结果（2026-10-23）**

`uniqueCatalogID` 已改为接收 `map[string]struct{}`，两处调用点（恢复与导入）
各建一次集合；5000 主机启动从 **73.8 ms** 降到 **34.6 ms**（2.1×），分配量不变
（约 30.9 MB，剩下的是 SQLite/JSON 解码）。原始建议保留如下。

**建议**

- 用 `map[string]struct{}`（或 `map[string]uint32`）记录已用 CatalogID，
  `uniqueCatalogID` 改成 O(1) 查找；重复时再试 `_2`、`_3` 后缀；
- `catalogIDFor` 的 SHA256 + `fmt.Sprint` 只在 ID 缺失时调用（现状已如此），
  批量导入时可以把结果也放进同一个 map；
- 恢复路径的 JSON/SQLite 分配（每台约 6 KB）是下一层优化；修掉 O(n²) 后实测
  5000 主机 34.6 ms，常规规模（数百台）在噪声范围内。
- 批量导入/复制主机走的是同一个查重函数（`host_catalog_transfer.go`），修一处两条路径都受益；
- 加一个“5000 主机启动”的 benchmark 或上限测试，防止再次退化为平方复杂度。

### ~~3.7 T1（P2）一次 Wake 解析整段输出~~

**代码路径**

SSH 读侧最多缓存 1 MiB（[`shell.go:19`](../internal/sshconn/shell.go#L19)），
`pump` 一次 `Drain` 后把整段交给 `term.Write`，见
[`internal/workbench/terminal.go:356`](../internal/workbench/terminal.go#L356) 与
[`internal/term/parser.go:59`](../internal/term/parser.go#L59)。

**影响**

按 210 MB/s 的解析吞吐，1 MiB 需要约 **4.3 ms**；CJK 115 MB/s 时约 **8.7 ms**。
这是单帧一次性的窗口线程停顿，发生在“后台已经积压了一大段输出”的时候
（窗口最小化/被拖住/切换标签页之后回来），正是最容易感知卡顿的时刻。

**落地结果（2026-10-23）**

`pump` 每 Wake 最多解析 `pumpBudget = 128 KiB`；剩余量前移后调用
`state.options.Invalidate()` 申请下一帧继续。shell 结束、exec 捕获（其
`feedExec` 会复用缓冲区）和没有 Invalidator 的调用方仍一次处理完，保持旧行为。
解析器本身支持跨 Write 的转义序列拆分；测试把 `\x1b` 放在预算边界上，验证
下一帧续上后仍正确显示。1 MiB 积压从一次约 4.3 ms（CJK 8.7 ms）的停顿变为
8 帧、每帧约 0.6 ms。

**审计时的建议**

- `pump` 每帧只消费固定预算（如 64–128 KiB），把剩余留在 `t.rx`，再请求一次重绘继续；
  交互输出不受影响，积压输出在几帧内追平。
- 预算按解析耗时而不是字节数更稳（CJK/宽字符不同）。
- 加一个“积压 1 MiB 后 Wake”的 benchmark 断言单次 `pump` < 预算。

### ~~3.8 T2（P2）终端解析器的两处结构性成本~~（ASCII 已完成；环形屏未采用）

1. **滚屏复制整屏指针**：`rotate`
   （[`terminal.go:499`](../internal/term/terminal.go#L499)）每滚一行执行一次
   `copy(lines[top:bottom], ...)`。审计时 profile 的 `rotate` cum 为 **21.9%**，但
   其中包含它调用的 `pushHistory`/`recycle`；纯指针复制只对应约 5% 的 `memmove`。
   后来实现了完整的环形 screen（head + `lineAt` + 区域滚动 linearize）并通过全部
   测试，纯文本吞吐 298.6 MB/s，与线性屏幕的 304.6 MB/s 在噪声范围内，无可测收益，
   因此**未采用**，保留更简单的线性实现。若未来行数/屏幕尺寸显著增大，再重新评估。
2. **ASCII 逐字符写**：`printASCII`
   （[`print.go:6`](../internal/term/print.go#L6)）对每个字节做 `pend` 检查、
   `dropMarks`、`setHyperlink`、`version++`。对“无待换行、无 marks、无 hyperlink、
   无插入模式”的纯文本 run，可以一次填充整段、只 bump 一次 version，再做一次
   lastRune 更新。profile 里 `printASCII` 占 **27.5%**，批量路径预计能把它减半。
3. **窗口变高时 `popHistory` 反复整环归一化**：`resizeLines` 每拉一行调一次
   [`popHistory`](../internal/term/terminal.go#L425)，而它每次先
   [`normalizeHistory`](../internal/term/terminal.go#L450)（复制 5000 个指针 + 40 KB）。
   拉起 50 行就是 50 次 × 5000 指针。归一化一次即可。
4. **CJK 宽度**：`Width` 对宽字符走 `sort.Search`（81 段）+ 组合字符回退，
   profile 中 5%。可以在 CJK 常用区加一个小页表，或缓存上一次 rune 的宽度；
   优先级低。

**落地结果（2026-10-23，部分）**

纯文本的 ASCII run 现在在“无待换行、无插入、无 marks/hyperlink、无宽字符半格”时
一次填完整段、只 bump 一次行版本；不满足条件仍走原逐字符路径。纯文本解析从
**210 MB/s 提升到 305 MB/s**（约 1.45×），整屏重绘 186 → 205 MB/s；新增了长文本、
颜色和光标位置的测试。`resizeLines` 拉起历史行时也先归一化一次历史环，不再每行
复制 5000 个指针。环形屏幕行缓冲原型已实现并测试，但吞吐无可测提升，未采用（见 §10.14）。

这些优化的真实收益主要在 `cat` 大文件、粘贴大段文本、持续高吞吐输出；
对普通 SSH 交互（通常 <1 MB/s）影响有限，所以排在 P2。

### 3.9 M1（P3）内存预算

| 项 | 当前上限/实测 | 说明 |
|---|---|---|
| 回滚区 | 5000 行、**16 MiB/标签页**双上限；120/160 列满行仍可留满 5000 行，1000 列约 1042 行 | `Cell` 16 B；`scrollbackLines = 5000`（[`terminal.go:37`](../internal/workbench/terminal.go#L37)）；字节预算见 [`terminal.go`](../internal/term/terminal.go) `defaultHistoryBytes` |
| 终端图片 | 64 MB（[`image.go:23`](../internal/term/image.go#L23)） | 单图最大 4 M 像素 |
| 背景图 | 长边 2560、约 16 MB | 已离线程解码 |
| SFTP 列表缓存 | 16 个目录、总计约 **32 MB**（[`sftp.go:127`](../internal/workbench/sftp.go#L127)） | 条目含字符串，按估算字节淘汰 |
| SFTP 文本编辑器 | 1 MiB | `MaxEditFileBytes` |
| 录制 | 8 MiB / 65 536 事件 | 有界 |
| 终端输出缓冲 | 1 MiB / 会话（峰值 cap 4 MiB） | 有界，超限会释放 |

10 个满回滚区标签页约 90 MB（推算），加上图片/列表缓存，长期使用可到数百 MB。
`Scrollback` 设置提供 1000/5000/10000 三档；10000 行时单标签页约 18 MB。

**落地结果（2026-10-23，部分）**

SFTP 列表缓存现在同时按目录数（16）和估算字节（32 MB）淘汰；单目录超过整个预算
则不缓存，新读数会替换旧读数的字节计数。`TestSFTPListingCacheHasAByteBudget` 覆盖
淘汰、超大列表拒绝和 `forgetListings` 清零。图片上限未改。

回滚区现在同时受行数（`DefaultScrollback = 5000`）和 **16 MiB 字节预算**约束：每行按
`Cell`/`Mark`/hyperlink 的实际容量记账，超出时从最旧行开始回收（[`terminal.go`](../internal/term/terminal.go)
的 `trimHistory`）。满宽 80 列时 5000 行占 6.9 MB，120/160 列仍能留满 5000 行
（10.35 / 13.45 MB）；240 列约 4262 行（17.3 MB，按行数会到 20.4 MB）、500 列约
2072 行（17.0 MB，原 40.3 MB）、1000 列约 1042 行（17.7 MB，原 80.1 MB）。预算按
内容而不是终端宽度记账，所以短行日志仍保留完整行数。`TestWideHistoryStaysWithinItsByteBudget` 覆盖边界和
提高行数上限后仍不越界。

**审计时的建议**（不急）：

- 回滚区同时按字节预算（例如 16 MB/标签页）裁剪，而不是只看行数；
- 稀疏行（大量空格）用游程/空行共享表示，或把 `Cell` 的 Attr 打包进颜色字（改动大，
  需单独评估）；
- SFTP 列表缓存按字节预算淘汰，而不是只按目录数；
- 图片解码移出窗口线程（见 3.10）。

### 3.10 其他窗口线程阻塞点

- **终端图片解码**：[`decodeImage`](../internal/term/image.go#L320) 在
  `Terminal.Write` 里同步做 base64 + PNG/JPEG 解码（最大 4 M 像素）。一个 4 M 像素
  图片解码可达几十毫秒，期间窗口线程停住。建议像背景图一样离线程解码，回到窗口线程
  只做 `acceptImage`。上限已经存在（64 MB store、64 张图），所以这是延迟问题不是内存问题。
- **图片定位 O(图片数 × 回滚行数)**：[`ImageRow`](../internal/term/image.go#L84) 每次绘制
  对每个 placement 扫描整个 `hist`（最多 5000 行）。现在 `paintGrid` 把可见行的 line ID
  收进复用的 `rowIDs`，[`paintTerminalImages`](../internal/workbench/terminal_paint.go#L359)
  先线性查找这些 ~40 个 ID；只有锚点不在屏幕上的图片才回退到原来的 `ImageRow` 扫描。
- **命令历史写入**：已改为 `cmdhistory.Queue`：窗口线程只入队，后台 goroutine
  在 100 ms 窗口内合并提交；读取、删除、清空和关闭前先 flush。1000 条命令的
  基准从约 **993 ms**（逐条同步）降到 **12.8 ms**（一次批量事务）。
  错误仍会在下一次 `Record`/读取时显示；见 §10.16。
- **保存**与 **find** 已单列；**本地目录**已单列；**parse burst** 已单列。

### 3.11 A1/R1（P3）AI 与远程桌面

- AI 流：SSE 事件现在把 payload 作为解析器缓冲的 `[]byte` 直接交给 handler，
  不再每条事件做 `string`/`[]byte` 往返；`BenchmarkSSEParse1MiB` 从约 1.18 MB /
  18 k allocs 降到 **128 B / 2 allocs**；OpenAI handler 复用 chunk/choices 缓冲后，
  `BenchmarkOpenAIStream1MiB` 从约 12 MB / 239 k allocs 降到 **7.0 MB / 166 k allocs**。
  时间仍受负载影响；AI 响应通常几十到几百 KB，GC 压力有限。
- `Redact` 最坏路径（文本里真有 secret）93 ms/MiB、7.8 MB、71 k allocs；
  干净文本 5.7 ms/MiB、0 alloc。只有发送给模型前才调用，且 secret 是少数；
  可保持。
- `AppendMessage` 399 µs（SQLite），`ConversationsSearch` 907 µs；用户动作级，可接受。
- RDP：1080p 的未压缩 planar 与 NSCodec 原来约 **3.6 ms/帧、0 alloc**。现在把
  大帧的 R/G/B 交织和 YCoCg 转换按行分到最多 8 个 goroutine；RLE planar 先扫描
  三个平面的边界，再并行解码三个平面（alpha 预填也并行）。1080p **0.94/1.56 ms**，
  4K 未压缩 planar **7.4 ms**、NSCodec **4.5 ms**、4K RLE **9.4 ms**（当前负载下），
  代价约 11–17 allocs/帧；小图/小 tile 仍是单线程 0 alloc。
  4K 60 fps 的解码预算 16.7 ms，目前解码在预算内，但完整管线/真实窗口未测；
  SIMD 或硬件解码仍可作为后续。
- VNC 的 row/tile 缓冲已复用（`decode.go`），没有每帧分配问题。
- 云同步每次把整份加密文档下载、解密、合并、重新加密上传；配置含数百主机与密钥时
  是 MB 级网络量。已有 5 s 去抖且在网络 goroutine 上，暂可接受；若配置继续增长，
  值得改成按记录增量同步。

### 3.12 启动、二进制与依赖

- 启动 80–103 ms，其中 Go 包初始化 34 ms；`modernc.org/libc` 的 netdb 占 3.3 ms/3.6 MB，
  icons 2.2 ms，workbench 2.3 ms。Motty 自身状态 <1.2 ms。
- 21 MB（剥离）二进制对冷启动的 page-in 有影响，但本次没有测冷 page cache；
  如果将来要压缩启动时间，优先看图标数据包和 netdb 的按需化，而不是应用层。
- 空闲 work 已经很好：没有 ticker/polling 常驻；keepalive 每个连接一个 ticker（有连接时才有）；
  remote stats 只在用户打开监控时启动（流式命令、无轮询）；cursor blink 20 s 后停止请求帧。

## 4. 已经做得好的地方（不要回退）

- **空闲**：<0.01% CPU、19 个 goroutine、无 timer/netpoll 常驻；所有动画按需请求帧，
  blink 20 s 后停，spinner 只在 busy 时以 8 fps 请求。
- **绘制**：retained canvas + 行缓存（按 line ID/version/palette epoch），未变屏幕
  40 µs、0 alloc；多数页面 hover 帧 0 alloc；Viva 的文本 shaping、图标三角化、
  surface tile 都有缓存。
- **背压**：SSH/local/serial 输出都有 1 MiB 上限 + cond 阻塞读侧；输入 4 MiB 上限；
  超限的 buffer 峰值会释放，不永久保留。
- **有界缓存**：行缓存约 2 屏；SFTP 16×20k；图片 64 MB/64 张；录制 8 MiB/65 536 事件；
  AI tap 有 head/tail 截断。
- **网络**：keepalive/心跳是定时器而非轮询；SFTP worker 结果通道只保留最新一条，
  慢窗口不会阻塞服务器；remote stats 是一条流式命令而不是每秒开新会话。
- **终端解析**：纯 ASCII 快路径、行版本号、回滚区环形复用、无锁（单线程）设计；
  5000 行历史只分配约 9 MB，且不随输出时长增长。

这些是这次排查里“看起来该有、实际已经有”的部分；优化时不要用更窄的本地补丁破坏它们。

## 5. 建议路线图

| 阶段 | 内容 | 预期指标 | 风险 |
|---|---|---|---|
| ~~1~~ | ~~H1：内置高亮预过滤（保语义）~~ | ~~`PrepareALogLine` 命中行 < 10 µs、普通行 < 1 µs；flood 帧 < 0.9 ms~~ | ~~低~~ |
| ~~2~~ | ~~H1 终态：字面量规则 + IPv4 扫描~~ | ~~单行 ~1 µs；flood 帧 ~0.55 ms；滚动新行 ~0.1 ms~~ | ~~中（等价性）~~ |
| ~~3~~ | ~~W1：保存去 JSON 往返 + 去抖~~ | ~~无变化 110 µs；单设置 1.08 ms；多字段也增量~~ | ~~基本完成~~ |
| ~~4~~ | ~~S2：列表渐进式 + 链接后台解析~~ | ~~首屏 4 RTT（ReadDir + 账户文件）；200 链接不阻塞列表~~ | ~~已完成~~ |
| ~~5~~ | ~~S1：并发读写 + 恢复校验并发~~ | ~~下载/上传/续传校验均已并发~~ | ~~已完成~~ |
| ~~6~~ | ~~W2：find 去抖/增量~~ | ~~流式输出时每帧无 `Search`~~ | ~~低~~ |
| ~~7~~ | ~~W3/T1：本地列表/行预处理/图片解码/解析预算~~ | ~~T1 完成；W3 扫描/排序/行准备已离线程~~ | ~~基本完成~~ |
| ~~7b~~ | ~~W4：恢复目录用集合查重~~ | ~~5000 主机 < 50 ms；消除 O(n²)~~ | ~~低~~ |
| 8 | T2/M1：环形屏、ASCII 批量、内存预算 | ASCII 完成；环形屏评估后未采用；SFTP 缓存有字节上限；回滚区已有 16 MiB 字节预算 | 完成 |
| 9 | V1/A1/R1 | 按产品需要 | 低 |

### 5.1 现有 benchmark 的缺口

| 缺口 | 建议加的 benchmark |
|---|---|
| SFTP 单文件吞吐、往返次数 | 高 RTT 夹具（延迟代理）+ 100 MB 下载/上传；统计每秒请求数 |
| 符号链接列目录 | 200 链接的 fake Remote，断言首屏只等 1 次 ReadDir |
| 保存的 alloc 预算 | 现有 `BenchmarkSave*` 加 `B/op`/`allocs/op` 门槛 |
| find + 流式输出 | 打开 find 后灌 1 MB/s，断言每帧不跑 `Search` |
| 本地大目录 | 2 万条 `listLocal`（枚举 + 排序 + 行准备）的耗时/分配 |
| 大主机目录启动 | 300/5000 台 `New`，断言无平方增长 |
| 回滚区内存 | 5000/10000 行 × 不同列宽的堆增量 |
| 真实窗口帧时间 | Viva `benchmarks/typing_latency`（需要真机），本次未运行 |

**验收方式**：每个阶段先加/改 benchmark，记录 before/after 的 ns/op、B/op、allocs/op；
UI 路径再用无头 `layout`/`texts` 或 `shot` 验证行为不变；涉及终端语义的加拆分输入测试，
涉及字节协议的加 fuzz。跨仓库（V1）按 AGENTS 的 Viva 流程另开任务，不混在 Motty 的 PR 里。

## 6. 复现

```sh
# 全量 benchmark（临时 GOCACHE 以避开沙箱对 ~/Library/Caches 的限制）
export GOCACHE=/tmp/motty-gocache
go test -tags vivahead ./... -run '^$' -bench . -benchmem -benchtime 200ms -count 1

# 关键热点 + profile
go test -tags vivahead ./internal/workbench -run '^$' \
  -bench 'BenchmarkPaintTerminalFlood$|BenchmarkPrepareALogLine$|BenchmarkSaveNothingShared$' \
  -benchtime 3s -cpuprofile /tmp/motty.cpu -memprofile /tmp/motty.mem
go tool pprof -top -nodecount=30 /tmp/motty.cpu
go tool pprof -top -alloc_space -nodecount=20 /tmp/motty.mem

# 终端解析
go test ./internal/term -run '^$' -bench 'BenchmarkWrite' -benchtime 3s \
  -cpuprofile /tmp/term.cpu -memprofile /tmp/term.mem

# 无头启动/空闲（脚本思路）
#   全新 HOME 启动 /tmp/motty-head，读 "ok ready"；
#   用 libproc 的 proc_pid_rusage 每秒采 RSS 与 user+system CPU；
#   2 s 后 SIGQUIT 取 goroutine dump 统计 timer/netpoll。
```

注意：`BenchmarkPrepareALogLine` 的 “no rules” 与 “builtin rules” 差值就是高亮成本；
`BenchmarkPaintTerminalFlood` 的 profile 里看 `markCells` 的 cum%。

## 7. 关键 benchmark 摘录（200 ms，单次）

```
term:
BenchmarkWritePlainText-10       484252 ns/op   209.5 cpu-MB/s   19792 B/op   20 allocs/op
BenchmarkWriteColoredText-10     756469 ns/op   158.3 cpu-MB/s   17678 B/op   31 allocs/op
BenchmarkWriteCJKText-10        1708897 ns/op   114.9 cpu-MB/s   51185 B/op   72 allocs/op
BenchmarkWriteFullScreenRedraw   13519 ns/op   186.4 cpu-MB/s       0 B/op    0 allocs/op
BenchmarkSearchScrollback      1621805 ns/op                         272 B/op    2 allocs/op
BenchmarkLinksScrollback       1251394 ns/op                           0 B/op    0 allocs/op

workbench:
BenchmarkSaveNothingShared      1351902 ns/op   697253 B/op   9132 allocs/op
BenchmarkSaveOneHostChanged     2179296 ns/op   874127 B/op  11329 allocs/op
BenchmarkPrepareALogLine/no_rules      2079 ns/op      96 B/op    1 allocs/op
BenchmarkPrepareALogLine/builtin_rules 22718 ns/op    644 B/op    5 allocs/op
BenchmarkPaintTerminalFlood       1127155 ns/op   58126 B/op  103 allocs/op
BenchmarkPaintTerminalUnchanged     40497 ns/op       0 B/op    0 allocs/op
BenchmarkWholeWindowPaintThroughTheDriver 209189 ns/op 2976 B/op 4 allocs/op
BenchmarkKeystrokeToEcho           201852 ns/op     151 B/op    4 allocs/op
BenchmarkPaintSFTPPage5000          94247 ns/op       0 B/op    0 allocs/op

sftpclient:
BenchmarkEntriesFrom                24025 ns/op   18432 B/op    1 allocs/op
BenchmarkDecodeTextDocument          9841 ns/op  1092.56 MB/s 10880 B/op 1 allocs/op

ai:
BenchmarkOpenAIStream1MiB       21063342 ns/op   49.79 MB/s  12037278 B/op 239185 allocs/op
BenchmarkSSEParse1MiB            1124017 ns/op  932.93 MB/s   1177540 B/op  18399 allocs/op
BenchmarkRedact1MiB             93337250 ns/op   11.23 MB/s   7874018 B/op  71422 allocs/op
BenchmarkRedactClean1MiB         5707566 ns/op  183.72 MB/s    26136 B/op      0 allocs/op

rdpcore:
BenchmarkDecompress_1920x1080    3591929 ns/op   251469 B/op      0 allocs/op
BenchmarkDecompressPlanar_1920x1080 3675594 ns/op 129664 B/op    0 allocs/op
BenchmarkDecodeTileData            77023 ns/op    16422 B/op      2 allocs/op
BenchmarkDecodeTileDataParallel    12452 ns/op    16843 B/op      2 allocs/op
```

`BenchmarkPaintTerminalFlood` 的分配（58 KB/帧）主要来自每行一份 `string(text)`；
在高刷新率下约 3.5 MB/s 垃圾。它不是当前瓶颈，H1 之后再评估是否值得为
`LabelRuns` 增加“字节文本”或行文本池（会触碰 Viva API，单独评估）。

## 8. 未覆盖 / 未验证

- **真实窗口**：按 AGENTS 未打开真实应用窗口；CoreText 排版、Metal 呈现、vsync、
  窗口拖动时的 live resize 行为都没有实测。本文的绘制数字是无头 painter/driver 的
  CPU 成本，真实 GPU 呈现会另有开销。
- **平台**：Motty 只在 macOS/arm64 上跑过完整测试；Viva 的 `cmd/viva-measure` 已在
  Linux 和 Windows 测试机上跑过真实窗口启动/idle，但 Motty 的 Linux/Windows 文本引擎、
  DPI、GPU 后端和真实窗口仍没有验证。
- **广域网**：SFTP 的 RTT 表是代码与包语义推算；符号链接往返是 2 ms 模拟 RTT；
  没有在真实 50/100 ms 链路上跑过。`e2esshd` 回环测不出这个差距。
- **内存**：9 MB/5000×120 是单终端探针；多标签页、图片、列表缓存叠加后的进程 RSS 未实测。
- **RDP/VNC**：只跑了协议解码 benchmark，没有真实远程桌面会话的端到端帧率/延迟。
- **功耗/电池**：未测 energy impact；空闲 0% 是 CPU 口径。
- **门禁**：最终结果见 §11。`gofmt -l .`、`go vet ./...` 通过；`go test ./...`
  只有 `internal/localshell`（pty `operation not permitted`）和
  `internal/remotetools`（`/bin/ps` 被沙箱拒绝）失败，其余 67 个包通过；
  `go test -race ./...` 跳过这两项与 workbench 的本地 pty/背景图用例后通过。
  这些是沙箱环境限制，在**未改动的 HEAD** 上同样复现。

## 9. 附：包覆盖情况

| 包 | 结论 |
|---|---|
| `term` | T2 批量 ASCII 已落地；环形屏评估后未采用；图片/内存预算已记录 |
| `workbench` | H1/W1/W2/W3/W4/T1 已落地；W1 常规保存全部增量；本地/远程大目录枚举与行准备离线程 |
| `sshconn` | 读缓冲、keepalive、输入队列都无每字节分配；未发现热点 |
| `sftpclient` | S1/S2 均已落地；高 RTT 真机吞吐待实测 |
| `scpclient` | 64 KiB 缓冲；SCP 协议本身串行，无额外发现 |
| `rfb` | row/tile 复用；无每帧分配热点 |
| `rdpcore` | 1080p planar/NSCodec 已并行交织/转换到 0.94/1.56 ms；更高帧率/分辨率仍属协议成本 |
| `desktop` | frame update+present 800 ns；tile surface 设计良好 |
| `ai/*` | A1 已落地；SSE 事件零复制，OpenAI chunk 复用，流分配仍偏高但可接受 |
| `workspace` | 布局 2–9 µs；fine |
| `fuzzy` | 5000 条 1.25 ms；命令历史 picker 可接受 |
| `highlight` | H1 |
| `cmdhistory` | 每键快路径；`Record` 已排队到后台批量提交（默认关闭） |
| `knownhosts` | 页面打开时解析/指纹全部条目；常规规模 fine |
| `configstore` | SQLite 提交；W1 的提交部分不是瓶颈 |
| `vault` | 按需 AES；fine |
| `localshell`/`serial`/`telnet` | 1 MiB 背压 + 32/16 KiB 读；fine |
| `portforward`/`x11fwd` | `io.Copy`；fine |
| `xymodem`/`zmodem` | 串口速率受限；fine |
| `update`/`cloudsync`/`gist`/`s3`/`webdav`/`backup` | 用户/定时触发的网络工作，无空闲轮询；cloudsync 是整份文档同步（见 §3.11） |
| `sessionimport`/`host import`/`otp`/`keyidentity` | 用户动作级；`Inspect` 41.6 µs/次 |

## 10. 实施记录（2026-10-23）

已落地 H1、W4、W2 与 W3 的排序部分，其余按 §5 继续。

### 10.1 H1：内置高亮的字面量快路径

- 代码：[`internal/highlight/highlight.go`](../internal/highlight/highlight.go)；
  `Builtin()` 的三条词规则在 `Compile` 时同时保留正则（非 ASCII/超长行回退）和
  字面量匹配器（ASCII 行）。
- 语义：完全复刻 `\b`、大小写不敏感、规则优先级和“已标记字节不覆盖”；
  `active (running)` 的原有边界行为也保留。正则模式字符串由同一组词生成，
  `Validate` 与旧行为一致。
- 验证：`TestBuiltinFastPathMatchesTheExpressions`（手写 + 生成语料）、
  `TestBuiltinFastPathKeepsUserRulePriority`、`TestBuiltinFastPathHandlesLongLines`、
  `TestBuiltinFastPathMatchesTheQuadExpression`、`TestBuiltinMarkDoesNotAllocateOnASCIILines`、
  `FuzzBuiltinFastPathMatchesTheExpressions`（10 s，87 k execs）。
- 性能：见 §3.1 的落地表；flood 帧 1.1 ms → 0.33 ms。

### 10.2 W4：主机目录恢复从 O(n²) 改为集合查重

- 代码：[`internal/workbench/host_catalog.go:158`](../internal/workbench/host_catalog.go#L158)、
  [`internal/workbench/persist.go:587`](../internal/workbench/persist.go#L587)、
  [`internal/workbench/host_catalog_transfer.go:133`](../internal/workbench/host_catalog_transfer.go#L133)。
- 行为：重复 ID 仍按 `_2`、`_3` 递增；恢复与导入共用同一集合逻辑。
- 性能：5000 主机 `New` 73.8 ms → 34.6 ms；300 主机在噪声范围内（2.7–3.2 ms）。
- 验证：导入/恢复/唯一性用例通过；全包除上述环境相关的 pty/背景图用例外通过
  （两者在未改动 HEAD 上同样失败）。

### 10.3 W2：find 流式输出节流

- 代码：`internal/workbench/terminal_find.go`、`connection.go`、`terminal_paint.go`。
- 行为：流式输出下最多每 100 ms 重扫一次；打开/配置/resize/scrollback 变更仍立即扫描；
  `paintGrid` 在 pending 时申请补齐帧，输出停止后仍会追平。
- 验证：`TestTerminalFindThrottlesStreamingRescansAndCatchesUp`、
  `TestADeferredFindAsksForTheFrameThatCatchesUp`，以及原有 find 用例全部通过。

### 10.4 W3（部分）：本地目录排序

- 代码：`internal/workbench/sftp_local.go`。
- 行为：目录仍排在文件前；名字仍按大小写不敏感排序；新增
  `TestLocalPaneSortsNamesWithoutRegardToCase`。
- 性能：2 万条排序 5.8 ms → 2.5 ms（一次性探针测量，比较器分配消失）。
- 未完成：`os.ReadDir` + 每项 `Info()` + `prepareSFTPRows` 仍在窗口线程；见 §3.6。

### 10.5 S2：符号链接渐进式解析

- 代码：`internal/sftpclient/worker.go`（`list`/`describeOwners`/`resolveLinks`）、
  `internal/workbench/sftp.go`（FollowUp 处理）。
- 行为：首屏先出；链接在后台解析后以 `FollowUp` 原地补全；选择/滚动不动；
  取消时丢弃跟进；结果 channel 在跟进之后关闭。
- 性能：200 链接、2 ms RTT 下首屏 1 003 ms → 9 ms；40 ms RTT 外推约 16 s → 0.16 s。
- 验证：`internal/sftpclient` 与 `internal/workbench` 的 SFTP 用例、race、以及
  `TestSFTPLinksFillInAfterTheListingIsShown` 均通过。

### 10.6 S1（部分）：SFTP 流水线传输

- 代码：`internal/sftpclient/worker.go`（`downloadCopy`/`uploadCopy`/进度包装）、
  `download_copy_test.go`、`upload_copy_test.go`。
- 行为：下载用 `File.WriteTo`，上传用 16 路 `ReadFromWithConcurrency`；进度、暂停、
  取消仍按块工作；不支持流水线的后端和“写出前失败”的服务器回退到顺序复制。
- 续传校验：8 路并发，顺序回退；测试覆盖重叠、mismatch、fallback、cancel。
- 未完成：高 RTT 真机吞吐实测待补。

### 10.7 T1：终端解析按帧限额

- 代码：`internal/workbench/terminal.go`（`pumpBudget`、`pump`）。
- 行为：每 Wake 最多 128 KiB；余量留在 `t.rx`，申请下一帧；shell 结束/exec/无
  Invalidator 时一次处理完。
- 验证：`TestALargeFloodIsParsedOverSeveralWakes`（转义序列跨边界）与
  `TestALargeFloodIsParsedInOneWakeWithoutAnInvalidator`；完整 workbench 用例通过。

### 10.8 T2（部分）：批量 ASCII 与历史归一化

- 代码：`internal/term/print.go`（`fillASCII`）、`internal/term/terminal.go`（`resizeLines`）。
- 行为：满足条件的整段 ASCII 一次写入；行版本只 bump 一次；不满足条件完全回退。
  `popHistory` 批量拉取前归一化一次，消除每行 5000 指针的重复复制。
- 性能：纯文本 210 → 305 MB/s；整屏重绘 186 → 205 MB/s（`BenchmarkWrite*`）。
- 未完成：屏幕行环形缓冲（`rotate`），CJK 宽度表。
- 验证：`TestALongPlainASCIITextRunKeepsItsCellsAndColor`、完整 `internal/term` 测试与
  `-race` 通过，workbench 全量（跳过环境用例）通过。

### 10.9 S1 续传校验

- 代码：`internal/sftpclient/worker.go`（`verifyPrefix`、`verifyPrefixParallel`、
  `verifyPrefixSequential`）、`verify_test.go`。
- 行为：两端可随机读时 8 路 × 64 KiB 并发比较，完成后 Seek 到前缀末尾；不支持时
  回退顺序；暂停/取消按块生效。
- 验证：tracking ReaderAt 证明并发重叠，另有 mismatch/fallback/cancel 和完整 resume 用例。

### 10.10 W1：增量保存

- 代码：`internal/workbench/persist.go`、`persist_records.go`、`persist_incremental.go`、`workbench.go`。
- 行为：快照相同直接返回；一个或多个顶层字段变化时复用其他记录，只编码变化的列表、
  设置和本机值键，一次提交；`host_groups` 也增量处理（记录去折叠、本机存
  `collapsed_groups`）；只有状态版本字段回退完整 `splitState`。
- 性能（300 主机）：无变化 1.35 ms → 110 µs（708 KB → 84 KB）；单个设置
  1.08 ms / 301 KB（完整路径 2.99 ms / 900 KB）；单个本机值 0.63 ms / 90 KB；
  单个主机 2.46 ms / 792 KB。新增 `BenchmarkSaveOneSettingChanged` 与
  `BenchmarkSaveOneDeviceValue`。
- 验证：`TestChangedSavedFieldsRecognizesOneFieldChanges`、
  `TestSavingOneSettingUsesTheIncrementalPath`、`TestSavingOneDeviceValueUsesTheLocalShortcut`、
  `TestSavingSeveralFieldsAtOnceKeepsAll`、完整 workbench 用例（跳过环境相关 pty/背景图）。
- 未完成：状态版本字段仍走完整 `splitState`（正常保存不会变化）；其余常规保存均已
  增量。完整结构化 encoder 可作为后续整理。

### 10.11 W3：本地大目录离线程

- 代码：`internal/workbench/sftp_local.go`、`sftp.go`（`localSeq`）。
- 行为：本地只同步读前 513 项，大目录的剩余枚举/Info/链接/排序/行准备都在
  goroutine；远程列表结果超过 512 项时（包括有链接的列表）把 `prepareSFTPRows`
  移到 goroutine。`localSeq`/`remoteSeq` 丢弃过期结果；链接 follow-up 早到会在
  准备回调里优先使用。
- 验证：`TestALargeLocalListingIsScannedInTheBackground`、`TestAnOvertakenLocalListingIsDropped`、
  `TestALargeRemoteListingIsPreparedInTheBackground`、`TestAnOvertakenRemoteListingIsDropped`、
  `TestALargeLinkedListingResolvesAfterItIsShown`，以及原有本地面板用例。
- 未完成：超大目录仍可考虑分页/流式行；链接 follow-up 早到时重准备行仍可能在
  窗口线程花几毫秒。

### 10.12 M1（部分）：SFTP 列表缓存字节预算

- 代码：`internal/workbench/sftp.go`。
- 行为：缓存同时受 16 个目录和约 32 MB 估算字节约束；新列表替换旧列表的计数；
  大于预算的单个目录不缓存；`forgetListings` 清零。
- 验证：`TestSFTPListingCacheHasAByteBudget`。
- 未完成：回滚区仍按行数，图片仍按 64 MB 上限。

### 10.13 A1：SSE 事件复用解析缓冲

- 代码：`internal/ai/llm/sse.go`、`openai.go`、`anthropic.go`。
- 行为：`sseEvent.Data` 是解析器缓冲的 `[]byte`，仅在 emit 回调期间有效；handler
  直接 `json.Unmarshal`，不再复制成 `string` 再转回 `[]byte`。
- 性能：SSE 解析 1.18 MB/18 k allocs → **128 B/2 allocs**；OpenAI 流 12 MB/239 k →
  9.7 MB/202 k per MiB。
- 验证：`internal/ai/llm` 全量测试通过（测试在保存事件时复制 Data）。

### 10.14 T2：环形屏原型（评估后未采用）

- 原型：`screen` 增加 `head`、`line`/`setLine`，全屏滚动改移动 head；区域滚动、
  resize、CSI 擦除先 `linearize`。全部 `internal/term` 测试通过。
- 测量：纯文本 298.6 MB/s，线性实现 304.6 MB/s；差异在噪声内，`rotate` 的 cum
  主要来自 `pushHistory`/`recycle`，指针 `copy` 本身只占约 5%。
- 决定：回退原型，保留线性屏幕，避免为无收益的复杂度承担风险；审计文档记录该结论。

### 10.15 图片定位：可见行 ID 查找

- 代码：`internal/workbench/terminal.go`（`rowIDs`）、`terminal_paint.go`（`visibleRow`）。
- 行为：每帧复用可见行 ID 列表；图片锚点在可见行时线性查找，离屏锚点回退
  `ImageRow`。列表容量按屏幕行数保留，稳态不分配。
- 验证：终端图片绘制/0-alloc 用例与完整 workbench 用例通过。

### 10.16 命令历史：后台排队与批量提交

- 代码：`internal/cmdhistory/queue.go`、`internal/workbench/history.go`。
- 行为：`Record` 只入队；单个后台 goroutine 每 100 ms 合并一次，`List`/`Delete`/
  `Clear`/`Len`/`Close` 先 flush；失败批次保留待重试，错误可通过 `Error` 查询。
- 性能：1000 条命令从约 **993 ms** 降到 **12.8 ms**；单次 Enter 不再在窗口线程
  等待 SQLite 提交。
- 验证：`TestQueueFlushesOnReadAndClose`、`TestQueueRejectsAnEmptyCommand`、
  `BenchmarkQueueRecordBurst`，以及现有 history/ghost 用例。

### 10.17 R1：1080p/4K 解码并行化

- 代码：`internal/rdpcore/protocol/rle/planar.go`、
  `internal/rdpcore/protocol/nscodec/nscodec.go`。
- 行为：planar 的未压缩三平面交织、NSCodec 的 YCoCg direct/subsampled 转换在
  ≥1M 像素时按行分给最多 8 个 goroutine；planar RLE 先扫描 R/G/B 平面边界，再与
  alpha 预填并行解码三个平面。小图保持单线程。
- 性能（当前负载）：planar 1080p **0.94 ms**、4K **7.4 ms**；NSCodec 1080p
  **1.56 ms**、4K **4.5 ms**；4K RLE **28.7 → 9.4 ms**。约 11–17 allocs/帧。
- 验证：planar/NSCodec 全量测试、并行与顺序结果对比、`-race`，以及
  `FuzzPlanar`/`FuzzNSCodec` 各 5 秒；新增 4K 未压缩和 RLE benchmark。

### 10.18 A1：OpenAI chunk 复用

- 代码：`internal/ai/llm/openai.go`。
- 行为：`oaHandler` 复用 `oaChunk`，每事件先清空 choices 容量内元素、usage/error，
  再 `json.Unmarshal`；流式事件缺省字段不会沿用上一事件。
- 性能：`BenchmarkOpenAIStream1MiB` 9.7 MB / 202k allocs → **7.0 MB / 166k allocs**；
  SSE 解析仍是 128 B / 2 allocs。
- 验证：`TestOpenAIReusedChunkDoesNotLeakFields` 与 `internal/ai/llm` 全量测试。

### 10.19 待办

按 §5 的剩余阶段：M1 的回滚区/图片预算、V1；W1 仅剩 host_groups 回退完整路径。
R1 的 4K/60 fps、启动期 netdb/图标按需化仍可继续。每项落地时同样补测试、benchmark
与门禁。

## 11. 收尾

2026-10-23，经所有者确认收尾性能目标。可直接落地的 P0–P2 项均已实现并有
测试/benchmark 记录（§10）；T2 环形屏经原型测量无收益，已回退并记录（§10.14）。

明确留作后续的项：

- **M1 回滚区按字节裁剪**：会改变用户可见的保留策略，留待产品决策，不在本次
  静默改动。
- **R1 RDP 解码**、**V1 Viva 图标重放**：分别是独立的大工程和跨仓库能力，
  不在本次收尾范围内。

收尾门禁（本机沙箱）：

- `gofmt -l .` 无输出；`go vet ./...` 通过。
- `go test ./... -count=1` 除 `internal/localshell`（pty `operation not permitted`）
  和 `internal/remotetools`（`/bin/ps` 被沙箱拒绝）外全部通过；这两项是环境限制，
  与本次改动无关。
- `go test -race ./...` 在跳过上述沙箱用例及 workbench 的本地 pty/背景图用例后
  全部通过。

> 2026-10-23 第二轮：所有者追加了 3/4/5/7，见 §12。R1 的 1080p 与 V1 已有
> 实现（V1 在隔离分支），M1、4K/60 fps 与启动按需化仍待。

## 12. 追加范围（2026-10-23 第二轮）

所有者要求继续做“真正还没做”清单的第 3、4、5、7 项。

### 12.1 第 3 项：命令历史写入

已落地，提交 `2fd5799`。`cmdhistory.Queue` 在窗口线程只入队，后台 goroutine
100 ms 合并提交；`List`/`Delete`/`Clear`/`Len`/`Close` 先 flush。1000 条命令
从约 993 ms（逐条同步）降到 12.8 ms（一次批量事务）。

### 12.2 第 4 项：RDP 1080p 解码

已落地，提交 `9222182`。未压缩 planar 三平面一次交织，NSCodec 的 YCoCg
转换按行分到最多 4 个 goroutine；小图仍单线程。1080p planar 约
3.1–3.8 ms → **0.94 ms**，NSCodec 3.6 → **1.56 ms**，约 9 allocs/帧。
4K/60 fps 仍需后续。

### 12.3 第 5 项：Viva 图标重放

已在**隔离 Viva 分支**实现，尚未合并：

- 工作树：`/Users/skyler/VsCodeProjects/wt/perf-icons/viva`
- 分支/提交：`perf-icon-replay` / `0dc103c`
- 远端：已推送到 `origin/perf-icon-replay`；PR 入口
  https://github.com/vivaui/viva/pull/new/perf-icon-replay
- 改动：缓存按画布状态（origin/tint/layer/clip/rectsBefore）物化后的完整
  `[]Triangle`，重复绘制变成一次 slice 拷贝；两个缓存都保持 512 项上限。
- 性能：`BenchmarkDrawIconCached` 1022 → **297 ns**（3.4×），渐变重放
  1056 → **207 ns**（5×）；`BenchmarkPainterIconPageHoverToggle` 115.0 →
  **108.6 µs**。
- 验证：Viva `gofmt`、`go vet ./...`、`go test ./...`、`go vet -tags vivahead`、
  Windows/wasm vet、`go test -race ./ui` 通过；配对 Motty 工作树
  `/Users/skyler/VsCodeProjects/wt/perf-icons/motty` 的 headless build 与
  workbench 用例/驱动 benchmark 通过。
- **合并结果**：已通过 ff-only 合并进**本地 Viva main**（`0dc103c`）。合并前把共享
  检出中来源不明的未提交工作保存到 WIP 分支 `wip/viva-uncommitted-2026-10-23`
  （`50ed0ab`），未合并、未推送；该工作包含平台后端、painter/slider、CI、
  `cmd/viva-measure` 和文档，测试通过但不代表本仓库验收。
- **未推送 Viva main**：`origin/main` 仍在 `1a80483`，本地 main 比它多 `3df0ccb`
  （另一个已在本地的组件提交）和 `0dc103c`；直接 push main 会一并发布别人的未推送
  提交，故保留本地合并，`origin/perf-icon-replay` 分支已推送供 PR。
- **注意**：`tools/testenv/README.md` 含测试环境地址和密码，仍保持未跟踪，**不得提交**
  到仓库；共享 Viva 工作区因此还有一个未跟踪文件。
  本项没有 Motty 代码改动，因此没有 Motty 配对提交。

### 12.4 第 7 项：AI 流分配（启动项仍待）

已落地 AI 流部分，提交 `187fd43`。OpenAI handler 复用 chunk/choices 缓冲；
`BenchmarkOpenAIStream1MiB` 9.7 MB / 202k allocs → **7.0 MB / 166k allocs**。

启动部分先做了干净测量：`GODEBUG=inittrace=1` 显示包初始化总计约 20 ms
（冷 / 热），其中 `netdb` 约 **4.5 ms / 3.66 MB**、`workbench/icons` **0.87 ms**
（图标数据在二进制里约 **0.4 MB**）、`workbench` 0.22 ms。直接测量 `ok ready`：
首轮冷启动 **452 ms**，随后四轮 **75.3 / 72.0 / 74.3 / 74.0 ms**（中位数 74.3 ms）。

结论：冷启动的大头不是应用包初始化，而是操作系统 dyld/文件缓存/真实窗口平台初始化；
把图标改成按需加载最多省约 0.9 ms 和 0.4 MB 数据，netdb 又是 sqlite 依赖层，
在当前证据下不值得引入生成器/依赖层改动。启动按需化因此**本轮不做代码改动**，
需要先有真实冷 page cache 的 profile 再决定。

### 12.5 验证

- Motty：`gofmt -l .` 无输出，`go vet ./...` 通过；全仓测试与 `-race` 在跳过
  已知沙箱用例（localshell pty、remotetools `/bin/ps`、workbench 本地 pty/背景图）
  后全部通过。
- Viva：完整 `gofmt`/`go vet`/`go test ./...`、跨平台 vet、ui race 通过（见 12.3）；
  合并后在共享 Viva main 上重跑，全部通过。Motty 共享 main 对合并后的 Viva 做
  headless build、workbench 用例和驱动 benchmark，通过。
- 真实 Linux 测试机（软件 Vulkan/lavapipe）：`cmd/viva-measure` 打开窗口
  **1164 ms**（软件 GPU）、首帧 **6.8 ms**、idle CPU **0.03%**、idle paint **0**、
  RSS **118 MB**、heap **5.3 MB**；验证了合并后的 Viva 在真实 X11 窗口上不空转。
- Windows 测试机（新地址记录在未跟踪的私有文档里）：窗口打开 **71.0 ms**、首帧
  **8.6 ms**、idle CPU **0%**、idle paint **0**、paints **4**、RSS **38 MB**、
  heap **0.38 MB**；GPU profile 关闭，运行在 WARP。
- RDP 4K 只有解码 benchmark，没有真实 4K RDP 会话/60 fps 端到端数据。

## 13. 复查落地（2026-10-27）

第二轮复查（HEAD `791f4b0`，0.1.3）确认基线没有回退：热启动 66–75 ms、常驻 50.7 MB、
空闲 CPU 0%、`PaintTerminalFlood` 309–325 µs、`SaveNothingShared` 106–112 µs。
本轮落地四项；测量都是 Apple M2 Pro、Go 1.26.5、`-tags vivahead` 或纯 `term` 包。

### 13.1 内联图片像素转换：去掉每像素一次分配

[`decodeImage`](../internal/term/image.go) 原来逐像素走 `image.Image.At(...).RGBA()`，
每个像素把一个 `color.Color` 装箱进接口，**1 alloc/像素**。现在按解码器实际返回的类型
直接按行拷贝（`*image.RGBA` 换 R/B、`*image.NRGBA` 用与 `color.NRGBA.RGBA` 相同的
整数公式、`*image.YCbCr` 用 `YCbCrToRGB`、`*image.Gray` 展开，其余回退到原循环）。

| 指标 | 落地前 | 落地后 |
|---|---|---|
| 1 MP PNG (`BenchmarkDecodeImage/png/1024`) | 23.7–25.2 ms / 12.6 MB / **1.05 M allocs** | **12.2 ms / 8.45 MB / 29 allocs** |
| 4 MP PNG (`png/2048`) | 94–100 ms / 50 MB / **4.19 M allocs** | **49.5 ms / 33.6 MB / 37 allocs** |
| 1 MP JPEG (`jpeg/1024`) | 未单独测；旧转换口径约 **1.05 M allocs** | **17.6 ms / 5.8 MB / 12 allocs** |
| 4 MP JPEG (`jpeg/2048`) | 未单独测 | **69.5 ms / 23.1 MB / 13 allocs** |

等价性：`TestImagePixelsMatchesTheGenericConversion` 对 RGBA/NRGBA/Gray/Gray16/Paletted/
YCbCr 四种采样比、奇数尺寸、非零原点和子图逐字节比较旧循环；`TestImagePixelsDoesNotAllocatePerPixel`
守住“每次调用最多一次分配”。

### 13.2 图片解码离开窗口线程

`Terminal.Write` 不再解码图片。解析阶段只做 base64/分块组装并登记一个 `imageJob`：

- 每帧 `Wake` 在 `pump` 开头调用 `Terminal.ApplyImages()`，按**解析顺序**应用已完成的
  任务；队首未完成时后面的任务不会越过它（`TestEarlierImageDecodeBlocksLaterApplication`）。
- 解析时先**预留** placement（像素为 nil），解码完成后再挂上像素。因此图片字节之后的
  擦除、resize、回滚区淘汰、kitty delete 仍然作用在这张图上（`TestImagePlacementReservedBeforeDecode`、
  `TestResizeDropsAPendingPlacement`、`TestDeletingAPendingImageAppliesAfterTheDecode`）。
- kitty 的 `a=p`/`a=d`/`a=q` 也进入同一队列，回复顺序与命令顺序一致；解码完成或就绪
  操作都会通过 `SetImageWake` 请求下一帧（`TestAReadyKittyOperationAsksForAFrame`）。
- 解析器把 OSC/DCS 缓冲区**移交**给解码器（`osc`/`dcs` 置 nil），不再复制大图；第二批
  控制字符串按可打印 run 一次追加，而不是逐字节 append（`TestControlStringsLoadInRuns`、
  `TestOverlongOSCIsIgnoredWhole`）。
- 队列有上限（8 个任务 / 96 MB 负载、2 路并行解码）：超出的图片按失败处理并给出 kitty
  错误回复，内存不随程序写图速度增长（`TestImageQueueDropsPastItsBudget`）。
- `Reset` 丢弃待应用任务与像素（`TestResetDiscardsPendingImageWork`）；锚定行已淘汰的
  图片不再留下 placement（`TestImageAnchoredToAnEvictedLineIsDropped`）。
- 4 MP PNG 序列（62 KB 文本）的 `Terminal.Write`：**约 0.28 ms**（解析 + 移交缓冲，
  不计后台解码）；此前同一路径还要在窗口线程上做 base64 + 解码，约 **95–100 ms**。

### 13.3 终端链接悬停只扫指针所在的软换行组

[`LinksAt`](../internal/term/links.go) 只扫描指针所在的软换行行组，并在找到覆盖指针的链接
时立刻停止；[`terminalLinks`](../internal/workbench/terminal_link.go) 不再保留整段回滚区的
链接列表，也不再因终端版本变化而整段重扫。指针停在一行里移动时：落在同一个链接内直接
命中；同一版本内已确认没有链接的行也记住（按行负缓存），不重复扫描。

| 指标 | 落地前 | 落地后 |
|---|---|---|
| 5000 行回滚区上的悬停（`BenchmarkLinksScrollback` → `BenchmarkLinksAtScrollback`） | **1.26 ms** / 0 allocs | **97.9 ns** / 0 allocs |
| OSC 8 链接悬停（`BenchmarkLinksOSC8Scrollback` 全扫 ≈1.0 ms） | ≈1.0 ms | 约 0.1 µs（单行组） |

等价性：`TestLinksAtMatchesLinksForEveryPosition` 在纯文本、软换行、OSC 8、宽字符+组合
标记、标点、回滚区六类终端上，对**每个缓冲区位置**比较 `LinksAt` 与完整 `Links` 的结果
（目标一致、有无一致）；`TestTerminalLinkHoverChecksTheNextLineAfterALinklessOne`
守住负缓存不会跨行误命中。

### 13.4 单主机保存只重编码变化的那台

[`appendHostEntries`](../internal/workbench/persist_incremental.go) 按键缓存每台主机上次
写入的记录（主机值 + ord），未变化的主机直接复用 `base.payloads` 里的字节；只有变化或新
加入的主机重新 `Marshal`。重复/匿名 ID 回退到原来的通用路径，保证记录键语义不变。
`sameSavedHost` 用逐字段比较替代 `reflect.DeepEqual`（后者每次调用都会装箱并建 visited
map）。`configBase.accept` 对已知 ord 不再逐条 `json.Unmarshal`。

| 场景 | 落地前 | 落地后 |
|---|---|---|
| 300 台，改一台（`BenchmarkSaveOneHostChanged`） | 2.4–3.0 ms / 814 KB / 11.3 k allocs | **0.75 ms / 217 KB / 147 allocs** |
| 1 000 台，改一台 | 7.5 ms / 2.8 MB | **1.0 ms / 770 KB** |
| 5 000 台，改一台 | **31.7 ms / 14.9 MB** | **3.45 ms / 3.4 MB** |
| 300 台，改一个设置 | 1.0–1.6 ms / 319 KB | **0.77 ms / 202 KB** |
| 无变化保存 | 106 µs / 101 KB / 4 allocs | 不变 |

剩下的时间（本机 0.75–0.93 ms，随提交耗时波动）里约 0.6 ms 是 SQLite 提交
（`syscall`），不是编码。缓存里保存的是编码后的 payload，所以一次提交失败后重试仍会写
新值，而不是 base 里留下的旧记录（`TestAFailedHostSaveIsWrittenByTheRetry`）。等价性：
`TestSavingOneHostKeepsEveryOtherEncodedRecord` 比较未变化主机 payload 的底层指针；
`TestSavingHostsMatchesAReopenedStore` 在增删改/代理/标签/多字段同时变化后重开 store，
逐台比较 `savedHost`；`TestSameSavedHostNoticesEveryField` 用反射遍历 `savedHost` 的每个
字段，防止以后新增字段漏出比较；`TestDuplicateHostIDsStillUseTheGenericEncoder` 守住回退
路径。

### 13.5 验证与未验证

- `gofmt -l .` 无输出；`go vet ./...` 通过；`go test ./... -count=1` 全仓通过；
  `go test -race -tags vivahead ./internal/term ./internal/workbench` 通过。
- 一次性探针已删除，未进仓库；改动未提交。
- **未验证**：真实窗口下的图片显示与悬停手感、4 MP 图片在真实 GPU 上的上屏时间、Windows/
  Linux 文本引擎；`stringRun` 的吞吐是解析器口径，未在真实窗口的输入法/拖动路径复测。

（完）
