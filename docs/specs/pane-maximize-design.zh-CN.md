# 分屏最大化、底部切换及布局恢复设计

日期：2026-11-18。状态：已实施；实际验证结果见 [验证记录](pane-maximize-verification.zh-CN.md)。
本文基于 Motty 与本地 nexterm 源码静态分析；截图定义交互目标，以下新增接口均为建议接口。

## 1. 目标与范围

在每个分屏标题栏的关闭按钮左侧增加最大化/还原按钮。最大化当前分屏后，在该标签页内容区底部显示所有分屏的切换标签；切换后继续最大化；还原后恢复原来的分屏树及比例。
每个顶层标签页独立保存显示模式、活动分屏和基础布局；切换标签页、再次显示已有窗口、退出后重新启动均应保留对应状态。
“最大化”只占满标签页的终端区域，不改变操作系统窗口大小，不关闭侧栏、工具栏或其他分屏连接。

| 用户要求 | 验收行为 |
|---|---|
| 标题栏按钮 | 点击任意分屏的按钮，最大化被点击分屏，而非之前获得焦点的分屏 |
| 快捷键 | Windows/Linux 默认 Ctrl+Shift+M，支持在现有键盘快捷键设置中修改 |
| 底部切换 | 展示该顶层标签页内全部分屏，选中项与实际输入目标一致 |
| 还原 | 恢复原树结构、分屏顺序、方向和比例；保留当前选中的分屏 |
| 标签页隔离 | A 最大化、B 平铺，来回切换不改变彼此模式 |
| 持久化 | 重启后同时恢复基础树、模式、顶层活动标签页及其活动分屏 |

## 2. 当前代码事实

| 事实 | 源码位置 |
|---|---|
| 顶层标签页对应 Workspace，Deck 管理独立分屏树与 ActivePane | `internal/workspace/deck.go:76`、`:91` |
| 当前上限为 32 个 Workspace、每个 12 个 Pane | `internal/workspace/deck.go:15` |
| WorkspaceDTO 已存 ActivePane、Layout、Panes 和 Context，但无最大化状态 | `internal/workspace/deck_dto.go:16` |
| Panes() 已支持工作区 Tabs 模式：活动分屏占满，其余分屏绘制矩形为零 | `internal/workbench/workspace_layout.go:40` |
| Tabs 模式来自 managedWorkspaceLayouts，不适合作为普通标签页最大化开关 | `internal/workbench/workspace_pane_tabs.go:18` |
| 现有工作区分屏切换条高 36，位于内容区顶部 | `internal/workbench/workspace_pane_tabs.go:9`、`:27` |
| 分屏标题栏的关闭按钮已有独立命中区域 | `internal/workbench/terminal_paint.go:373`、`:432` |
| 几何缓存按 WorkspaceID 与 Deck.Revision 更新 | `internal/workbench/workspace_ui.go:146` |
| 终端尺寸通过 paneFractionBounds 计算，不只依赖绘制矩形 | `internal/workbench/terminal.go:1459`、`:1491` |
| 保存时 SessionID 被映射为恢复序号，PaneID 保留 | `internal/workbench/workspace_layout.go:437` |
| Deck 已写入窗口恢复数据 | `internal/workbench/persist.go:771`、`persist_records.go:104` |
| 窗口记录支持多个窗口及开发/正式版分离 | `internal/workbench/window_records.go:12`、`:23` |
| 再次启动的“显示已有窗口”执行 Raise；“新建窗口”走另一路径 | `internal/workbench/launch.go:132` |

因此，已有“平铺结构持久化”可继续使用。本次新增的是最大化显示状态及完整的 UI、焦点、尺寸适配，不需要另建一套布局存储。

### 2.1 nexterm 可借鉴的实现

- `src/stores/layoutStore.ts:610`：最大化与还原按 tabId 修改布局状态。
- `src/components/LayoutGrid.tsx:624`：隐藏非目标 Pane，保留 Pane 实例。
- `src/components/FocusPaneTabs.tsx:16`：底部标签切换时同时更新最大化目标与活动 Pane。
- `src/stores/persistence/tabPersistence.ts:256`：序列化 maximized_pane_id。
- `src/hooks/useShortcuts.ts:222`：最大化快捷键为双向切换动作。
上述路径相对于 `/data/workspace/ai/nexterm`。Motty 使用 Go/Viva，应借鉴状态与交互，不移植 React 组件。

## 3. 核心架构决策

推荐采用“基础布局树 + Workspace 级最大化开关 + 已有 ActivePane”。

```text
Window
  Deck
    Workspace A: Layout + Panes + ActivePane + PaneMaximized
    Workspace B: Layout + Panes + ActivePane + PaneMaximized
```

基础 Layout 始终保留真实分屏树。最大化只改变显示投影，不删除节点、不将比例改为 1、不复制备用树。
显示目标始终是 ActivePane，因此不另存 MaximizedPaneID。点击底部标签只需走统一的 activatePane 链路，避免显示目标与键盘焦点分裂，也减少跨窗口 ID 重映射工作。

| 备选方案 | 取舍 |
|---|---|
| 把 Layout 改为单分屏，另存旧 Layout | 恢复、关闭和新增分屏容易导致两棵树不一致，放弃 |
| 复用 managedWorkspaceLayouts 的 LayoutTabs | 将临时最大化与基础布局类型混在一起，且普通标签页并非 managed workspace，放弃 |
| 单独保存 MaximizedPaneID | 可以实现，但与 ActivePane 重复，所有焦点路径都要同步，不采用 |
| bool 开关叠加在原树上 | 状态最少，复用现有持久化与激活路径，采用 |

### 3.1 建议数据结构与接口

在 workspaceRecord 增加 `paneMaximized bool`；在 WorkspaceDTO 增加：

```go
PaneMaximized bool `json:"pane_maximized,omitempty"`
```

在 `internal/workspace/deck_view.go` 新增 `PaneMaximized(id) bool` 与 `SetPaneMaximized(id, enabled) bool`。
Setter 的返回值表示状态是否实际改变；改变时递增该 Workspace 的 revision；分屏数不足两个时归一化为 false。
WorkspaceInfo 如有绘制需要可暴露只读同名字段，但不得形成第二份可独立修改的状态。
新增 `State.setPaneMaximized(workspaceID, paneID, enabled)` 统一协调激活、几何更新、焦点及保存；快捷键通过 toggle 包装调用。

## 4. 行为与状态转换

| 当前状态 / 操作 | 结果 |
|---|---|
| 平铺，点击 Pane B 最大化 | 激活 B，开启当前 Workspace 最大化，显示底部条 |
| 最大化，点击底部 Pane C | 激活 C，继续最大化；B 会话保持运行 |
| 最大化，点击还原或再次按快捷键 | 关闭最大化，按基础树重新布局，C 保持活动 |
| 切换顶层标签页 | 读取目标 Workspace 自己的模式和 ActivePane |
| 最大化时关闭活动 Pane | 使用现有 Deck 关闭后的活动 Pane；剩余至少两个则继续最大化 |
| 剩余一个 Pane | 清除最大化状态，隐藏底部条及无意义的最大化按钮 |
| 最后一个 Pane 关闭 | 沿用现有关闭标签页/空标签页逻辑 |
| 最大化时执行新增分屏 | 成功后退出最大化，展示新增后的平铺布局，并按现有行为激活新 Pane |
| 最大化时应用布局预设 | 成功后退出最大化，以新基础布局显示 |
| 整个标签页移动到另一窗口 | 连同基础树、ActivePane、最大化开关一起转移 |
| 单个 Pane 移出标签页 | 原页按剩余 Pane 数归一化；新单 Pane 标签页默认非最大化 |

新增分屏时应先基于“还原后的基础布局矩形”验证最小尺寸与分屏上限，成功再切换模式并修改树，不能用最大化矩形替代原分屏尺寸判断，也不能先退出最大化再发现分屏无法完成。
最大化期间不提供分屏拖拽换位/拖出入口，避免在隐藏目标上操作；整页标签拖拽保持现有行为。程序内部的 Pane 转移仍执行状态归一化。
底部条仅切换，不增加关闭、重排或独立拖出入口；关闭入口仍在当前 Pane 标题栏。

### 4.1 与已有 Tabs 布局兼容

新增 `workspaceSinglePaneView(id)`，返回“最大化开启，或基础布局为 Tabs”。它只表示绘制方式，不能替代所有 workspaceTabbed 判断。
最大化开启时显示底部条，暂停原顶部 Pane 条及其高度预留，避免重复切换入口。
还原时恢复基础布局：普通分屏回到平铺，原本 LayoutTabs 的工作区回到顶部 Tabs。不得为了临时最大化覆盖用户保存的 LayoutSpec。
选择了 LayoutTabs 的工作区也可以使用最大化，以便临时把切换条移到底部；还原按钮始终可见。

## 5. 界面与几何

### 5.1 标题栏

在关闭按钮前增加最大化图标；最大化状态显示还原图标。工具提示分别为“最大化分屏”“还原分屏”，显示当前实际绑定的快捷键。
按钮使用现有主题色、图标按钮规格与独立 Hit ID；推荐以 PaneID 命名，保证空 Pane 也能操作。
点击非活动 Pane 的最大化按钮应先激活该 Pane；按钮命中不能触发标题栏拖拽，也不能落入关闭按钮。
普通终端入口在 `terminal_paint.go:373`；连接中、已断开、恢复待连接及远程桌面入口在 `connect_page.go:326`，空 Pane 在 `workspace_ui.go:219`。应提取可复用的 Pane 操作按钮组，逐类接入，避免仅支持已连接终端。
单 Pane 不显示最大化按钮；多 Pane 最大化后仍保留当前 Pane 标题栏及还原入口。

### 5.2 底部标签条

新增 `workspace_focus_tabs.go`。条高建议 36 个逻辑像素，与现有分屏 Tabs 一致；不硬编码物理像素。
顺序取 Layout 的视觉叶子顺序，与 Panes() 一致；用稳定 PaneID 定位动作，不持久化数组下标。
每项显示序号和名称，仅当前选中的分屏显示绿色圆点，用于标识活动分屏；名称复用会话 Title，空 Pane 显示“新标签页”。选中项使用主题强调色和可访问选中状态。
长名称省略并提供完整提示；空间不足时横向滚动，激活项自动滚入可见区，最多 12 项全部可达。
鼠标点击后将输入焦点还给目标 Pane；标签条本身获得键盘焦点时支持方向键选择、Enter 确认，再进入 Pane 内容。

### 5.3 内容区划分

从已有 workspacePixelBoundsFor 得到扣除侧栏、工具栏、日志区域等之后的分屏区域 F；最大化时进一步拆为：

```text
PaneContent = (F.X, F.Y, F.Width, max(0, F.Height - stripHeight))
BottomTabs  = (F.X, F.Y + PaneContent.Height, F.Width, stripHeight)
```

统一几何函数同时返回内容矩形与底部条矩形，供绘制、命中、终端尺寸和远程桌面使用，禁止不同调用点重复扣除高度。
底部条位于终端区域底边；命令抽屉、日志区或底部工具栏开启时，它位于这些现有区域之上。左右面板不被该条横跨或挤占。
只有终端页面声明底部条及其命中项；进入设置等页面时不得残留可点击区域。

### 5.4 绘制、尺寸与焦点必须一起修改

- `workspace_layout.go:40`：Panes 保留全部 Pane，最大化时仅 ActivePane 的可视 Bounds 为全区域，其余为零。
- `workspace_layout.go:61`、`workspace_ui.go:737`：单 Pane 显示时不生成分割线及 Splitter 命中区域，并取消正在进行的分割线拖动。
- `workspace_ui.go:146`：最大化 Setter 递增 revision，确保缓存下一帧重建；禁止只改 bool 而留下旧 panes 缓存。
- `workspace_ui.go:1179`：paneFractionBounds 的尺寸语义要同步更新；建议单 Pane 显示期间让全部 Pane 按完整内容区计算终端尺寸，与现有 Tabs 行为一致。
- `terminal.go:1491`：切换模式后调用 fitWorkspaceTerminals；底部切换不重建 Session，不断开 SSH/PTY，只适配需要变化的尺寸。
- 隐藏 Pane 的可绘制矩形为零，但 PTY 尺寸不能为零。不得直接用可视 Bounds 计算隐藏终端行列数。
- `workspace_ui.go:1167`：paneWindowBounds 用于窗口位置查询，应对隐藏 Pane 返回不可见；不能把“后台 PTY 全尺寸”误当作实际可命中区域。
- `workspace_layout.go:222`：统一使用 activatePane 同步 activeSession、监控目标、补全与查找；激活后下一帧请求目标内容焦点。
- `tab_drag.go:872`、`:898`：最大化时禁止分屏栏拖动与分屏边缘停靠；按钮用独立 Hit，底部标签不走顶层标签拖动流程。
- 远程桌面的鼠标坐标缩放必须使用最大化后的内容矩形；保持现有广播目标规则，最大化不隐式修改广播设置。

## 6. 快捷键与设置

新增 `shortcutPaneMaximize`，稳定序列化名称 `pane_maximize`，设置名称“最大化/还原当前分屏”。
Windows/Linux 使用 Ctrl+Shift+M；macOS 建议遵循现有 keymap.Primary 约定使用 Command+Shift+M，可在设置中改绑。
接入 `shortcuts.go` 的枚举、默认值、名称映射、执行分发，以及 `shortcut_paint.go`、`internal/i18n` 文案和命令面板。
枚举在末尾、Count 之前追加，避免无意改变现有动作编号。菜单、工具提示、设置展示统一读取实际绑定。
已有上一/下一分屏快捷键继续工作：默认 Primary+Alt+J/K；最大化时只切换活动 Pane，模式保持。Ctrl+Tab 继续切换顶层标签页。
快捷键遵循现有弹窗、输入控件和设置录制的路由优先级，防止录制按键时触发分屏最大化。
`persist.go:1338` 已按动作名恢复用户绑定后补默认值；若 Ctrl+Shift+M 已被用户占用，不抢占原动作，新动作保持未绑定并可手动配置。
同步更新 `website/content/docs/reference/shortcuts.mdx` 及中文文档。

## 7. 保存、恢复与兼容

### 7.1 保存边界

沿用 `State.snapshot → savedWorkspaceDeck → Deck.DTO → 窗口恢复记录`；新增字段位于 `deck.workspaces[]` 内，不成为全局设置，也不写入跨设备共享模板。
保存一份完整状态：基础树、Pane 绑定、ActivePane、PaneMaximized 同时进入快照，不能先保存模式、再单独保存活动 Pane。
保存触发：最大化、还原、底部切换、上一/下一 Pane、顶层标签切换，以及关闭、分屏、转移完成后。优先复用调用入口已有的 save，避免一次动作重复写盘。
底部 ui.Tabs 的 After 回调必须最终进入带保存的动作入口；当前 activatePane 自身不负责 save，不能假定点击后自动持久化。
无状态变化的重复点击不写盘；鼠标悬停、绘制循环、标题变化不新增布局保存动作。

建议新增字段后的示意数据（仅摘录新增字段相关部分）：

```json
{
  "id": 1,
  "active_pane": 3,
  "pane_maximized": true,
  "layout": "保留原有完整 Layout DTO，此处仅作占位说明"
}
```

### 7.2 恢复顺序

1. 加载原有窗口记录和会话元数据。
2. 保持 savedWorkspaceDeck/restoreWorkspaceDeck 的 SessionID 映射规则。
3. RestoreDeck 同时还原基础树、ActivePane 和 PaneMaximized。
4. 按剩余 Pane 数归一化最大化开关，恢复 managedWorkspaceLayouts 等附属状态。
5. 激活对应顶层标签页，计算内容区和底部条，刷新几何与焦点。
6. 首帧按已恢复模式显示；连接是否自动恢复继续服从现有启动连接设置。

恢复布局不等于恢复 Shell 进程、SSH 会话或终端滚屏。现有待连接页面也应能显示最大化与还原按钮。
原有瞬态容器会话过滤可能修改 ActivePane；bool 方案自动跟随过滤后的有效活动 Pane，无须再修补第二个目标 ID。
`deck_transfer.go:20` 的 Import 手工构造 workspaceRecord，必须显式复制新增 bool；Export 使用 DTO，DTO 新字段接入后跟随导出。
新窗口 Blank 路径继续丢弃旧 Deck（`workbench.go:1252`），不能错误继承另一个窗口的最大化状态。

### 7.3 两种“重新打开”

| 场景 | 行为 |
|---|---|
| 进程仍运行，再次点击程序图标，选择显示已有窗口 | Raise 当前窗口，保留内存状态，不重新读取旧快照 |
| 进程完全退出后重新启动 | 从每个窗口的 Deck 恢复每个标签页的模式与活动 Pane |
| 配置为新建窗口，进程运行时再次启动 | 沿用现有 Blank 新窗口行为，不复制旧窗口布局 |

不应把磁盘布局恢复绑定到 relaunchAction：“显示已有窗口”是再次启动策略，现有冷启动恢复机制是另一条独立路径。

### 7.4 格式兼容

字段默认 false，因此旧记录缺少字段时自然恢复成原有基础布局。新增可选 bool 不改变 Layout 的树格式，建议保持 DeckDTOVersion=1。
`docs/development.md:85` 说明当前存储允许兼容字段扩展，但数组作为整体处理；不能据此承诺旧版本重写 workspaces 数组后仍保留新字段。
兼容承诺为“新版读取旧记录并保持原布局”；降级到旧版可能丢失最大化标志，但基础树仍可用。实施时用隔离副本检查存储合并和版本校验路径，不修改真实用户数据库。
对于只有一个 Pane 的记录，读取时将 bool 归一化为 false；结构或引用损坏继续遵循既有 Deck 校验与恢复策略，不扩大本次存储修复范围。

## 8. 实施拆分

| 阶段 | 修改位置 | 完成标准 |
|---|---|---|
| 1. 状态模型 | `internal/workspace/deck.go`、新增 `deck_view.go`、`deck_dto.go`、`deck_transfer.go` | 每页独立 bool；revision 更新；DTO 和跨窗口导入往返保持状态 |
| 2. 统一显示判定 | `workspace_pane_tabs.go`、`workspace_layout.go`、新增 `workspace_focus.go` | 基础 Layout 与显示模式分离；操作统一经过激活、归一化、保存 |
| 3. 几何与输入 | `workspace_ui.go`、`terminal.go`、`tab_drag.go` | 可视矩形与 PTY 尺寸区分；隐藏 Pane 无命中；还原准确 |
| 4. UI | 新增 `workspace_focus_tabs.go`，修改 `terminal_paint.go`、`connect_page.go`、空 Pane 声明 | 标题栏按钮和底部条覆盖终端、连接页、空 Pane、桌面 |
| 5. 动作入口 | `workbench.go`、`shortcuts.go`、`shortcut_paint.go`、`command_palette.go`、i18n | 点击和快捷键调用同一动作，设置改绑、工具提示一致 |
| 6. 生命周期 | `workspace_arrangement.go`、分屏/关闭/移动入口、`workspace_layout.go` 恢复路径 | 新增、关闭、布局预设、跨窗口移动后状态正确 |
| 7. 验证与文档 | workspace/workbench 测试及快捷键文档 | 下列成功场景通过，截图确认，保留测试结果 |

阶段 1–3 先建立正确状态与几何，再接 UI。新文件及单次编辑均按不超过 100 行分批写入；仅暂存本任务文件。

## 9. 验证方案与验收清单

以下是设计时确定的验证方案，实际执行结果见验证记录。新增单元测试遵循项目偏好：只编写正常输入、正常操作成功场景；兼容策略描述不等于新增异常/边界测试。

| 成功场景 | 检查内容 |
|---|---|
| 三 Pane 非对称平铺 → 最大化 → 还原 | 完整 Layout DTO 在前后完全相等；方向、顺序、比例均相同 |
| 点击非活动 Pane 最大化 | 被点击 Pane 同时成为 ActivePane、activeSession 和输入目标 |
| 最大化后切换底部标签、上一/下一分屏 | 始终只绘制选中 Pane，底部选中态、焦点、实际输入一致 |
| A 最大化、B 平铺，切换后再重启 | 两页模式各自保持，活动页及各自 ActivePane 正确 |
| 最大化 DTO 序列化及 RestoreDeck 往返 | bool、ActivePane 与树均保持；原有旧格式正常记录仍可恢复 |
| 最大化标签页跨窗口移动 | 目标窗口 ID 重映射后，活动内容与模式保持 |
| 多 Pane 最大化时关闭其中一个，仍有多个 | 活动 Pane 按现有规则切换，仍最大化，其他连接继续运行 |
| 最大化时成功新增分屏 | 按基础矩形校验后完成分屏，显示新的平铺结构 |
| LayoutTabs → 最大化 → 还原 | 顶部/底部条互斥，恢复原 Tabs 基础布局 |
| 带侧栏、日志、命令抽屉时最大化 | 底部条不覆盖内容，终端行列数和实际区域一致 |
| 快捷键默认与成功改绑 | 新快捷键生效，按钮提示同步，原组合按设置释放 |
| 再次显示已有窗口 | 不重建连接、不从磁盘覆盖当前布局 |

自动验证建议：模型测试放在 `deck_view_test.go`，UI 与保存流程分别放在 `pane_maximize_headless_test.go`、`pane_maximize_persist_test.go`；命名仅为建议。
开发完成后先执行 `go test ./internal/workspace -count=1` 和 `go test -tags vivahead ./internal/workbench -run 'PaneMaximize|Workspace|PaneBar|Shortcut' -count=1`；再按仓库要求执行 `make check`，其中包含正式/开发构建测试与 race 检查。
真实窗口验收覆盖 Windows/Linux 的 Ctrl+Shift+M、macOS 对应绑定、DPI 缩放、底部溢出滚动、本地 Shell/SSH/远程桌面及恢复待连接页。用无敏感信息的开发配置截取平铺、最大化、切换、还原、重启恢复五组画面。

## 10. 风险与实施检查点

最高优先级风险是“界面变大但 PTY 未变大”以及“隐藏 Pane 仍参与命中”；通过统一几何入口、明确尺寸/可见性不同语义及输入验收消除。
其次是 revision 缓存未失效、回调忘记 save、跨窗口 Import 漏字段、Tabs 顶底条重复预留。各自已有明确修改入口和成功场景验证。
后台 Pane 全尺寸策略沿用现有 Tabs 模式，会在进入/退出最大化时引发后台 TUI resize；这是本方案的明确取舍，切换底部标签时可避免频繁 resize。
实施时仍需核实所有非终端 Pane 的标题栏组合与远程桌面尺寸回调，并在实际窗口验证 Viva Tabs 的滚动和焦点表现；静态分析不能替代这些运行验证。
本期不增加新的持久化数据库、跨设备同步布局、底部拖拽排序或 OS 全屏模式。
