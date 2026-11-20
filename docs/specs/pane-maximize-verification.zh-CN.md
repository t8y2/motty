# 分屏最大化实现与验证记录

日期：2026-11-18。功能已实现，代码已暂存，未提交。
设计：[分屏最大化、底部切换及布局恢复](pane-maximize-design.zh-CN.md)。

## 实现结果

- 各 Workspace 独立保存 `pane_maximized`，基础分屏树及比例始终保留。
- 分屏标题栏提供最大化/还原；底部条可切换、滚动，并显示序号和名称；仅当前选中分屏显示绿色圆点。
- Windows/Linux 默认 Ctrl+Shift+M，macOS 默认 Command+Shift+M；支持设置改绑。
- 覆盖本地终端、连接/恢复页面、空 Pane 和远程桌面；底部切换同步内容焦点。
- 模式、活动分屏及基础树可随窗口恢复；整个标签页跨窗口移动保留显示状态。
- 新增分屏成功后显示平铺；普通还原尊重原 Tabs 布局；剩一个 Pane 时取消最大化。
- 后台终端保持有效尺寸，隐藏 Pane 不参与命中，原有广播行为不变。
- 远程桌面捕获键盘时遵循原有转发规则，需释放捕获后使用应用快捷键，按钮仍可用。

## 验证环境

- Go：本机 `/soft/dev/go/bin/go`，版本 1.26.11。
- Viva：`../viva`，commit `4e8b0bbfb40976e7f6794b6efe08b6d16ae6d326`。
- Motty 基线：`c16f8eff`。
- Linux 下执行；未在真实 Windows/macOS 桌面运行本次改动。

## 已通过

- `go test ./internal/workspace ./internal/i18n -count=1`。
- 新增 12 个 PaneMaximize 成功场景测试，包括 DTO/Import、模式隔离、切换、输入、恢复、改绑、Tabs、滚动重入。
- 当前项目扩展回归：`go test -tags vivahead ./internal/workbench -run 'PaneMaximize|Workspace|PaneBar|Shortcut|SplitConnectionPanes|RestoredPanes' -count=1`，41.576 秒通过。
- `go test -race -tags vivahead ./internal/workspace ./internal/workbench -run 'PaneMaximize|WorkspaceMultiPaneSteadyPaintDoesNotAllocate|WorkspacePaintsEverySessionPageInsideItsPane' -count=1`。
- `make test-dev`。
- 初轮全仓库 `go vet`、开发标记 `go vet`；最终涉及的 workspace/workbench/i18n 包再次 `go vet` 通过。
- 正式桌面构建 `go build -o /tmp/motty-pane-maximize ./cmd/motty` 通过。
- headless 可执行文件构建、两组真实本地 Shell 交互验证通过。
- 独立需求审查、代码质量审查通过；`git diff --cached --check` 通过。

## 可复跑的端到端脚本

```bash
PATH=/soft/dev/go/bin:$PATH go build -tags vivahead -o /tmp/motty-pane-maximize-head ./cmd/motty
pane_test_home=$(mktemp -d)
MOTTY_HEAD=/tmp/motty-pane-maximize-head python3 tools/headless_drive.py "$pane_test_home" tools/e2e/pane_maximize_e2e.txt --fail-fast
MOTTY_HEAD=/tmp/motty-pane-maximize-head python3 tools/headless_drive.py "$pane_test_home" tools/e2e/pane_maximize_restore_e2e.txt --fail-fast
```

第二个脚本使用第一个脚本的同一临时 HOME，开启新进程验证恢复，再建立第二个标签页检查布局隔离。
截图输出在 `/tmp/motty-pane-{tiled,maximized,switched,restored,restarted}.png`。

## 全量检查限制

`make check` 在 workbench 包停止，以下三个既有测试在干净基线 worktree 上亦复现：

- `TestWindowAndHeadlessCatalogResolveEveryHostToTheSameConnection`：连接配置中的 SecurityKeyAgent 不一致。
- `TestSecondaryPressOnAnOpenMenuBackdropClosesItAndOpensTheHostMenu`：菜单未关闭。
- `TestChosenShellIsKeptByReconnectDuplicateAndRestart`：reconnect 空指针 panic。

因此不能宣称全量检查通过；`make check` 后续 shell 检查及全仓库 race 未由该命令执行，本次另行执行了开发测试和上述定向 race。
另跑 PaintGolden 时，修改前后出现完全相同的失败用例集合，涉及标题栏坐标等快照差异；没有批量重写现有 golden。
原始日志保留在 `/tmp/motty-pane-maximize-check.log`、`/tmp/motty-pane-baseline-failures.log`、`/tmp/motty-pane-golden.log` 与 `/tmp/motty-pane-golden-baseline.log`。
