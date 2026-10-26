# Windows 表单编辑回归

此分支统一使用 Viva 的普通文本输入处理，不在单个表单中拦截 Ctrl+V。
`go.mod` 临时固定修复分支的公开提交，避免仅在本机 `../viva` 含修改时生效。
上游 Viva PR 合并后，应将依赖切回包含该修复的官方版本。

## 开发与构建

```sh
go run ./cmd/motty
go test ./internal/workbench -run 'Test(WindowsHostForms|WindowsProviderForm|WindowsForm|HostFormEnter)' -count=1
```

继续开发本地 Viva 时，可在本机用 `go work` 引用 Motty 和相邻的 Viva checkout；
不要把机器相关的工作区文件或路径替换提交到 PR。
发布打包仍需要相邻的 Viva checkout 提供 CLI，运行时依赖以 `go.mod` 为准。

## 人工验证

- 新建、编辑、复制主机，以及设置 → AI 助手 → 添加服务商：普通字段与密码/API Key 字段均能粘贴中文、英文、长文本。
- Ctrl+A/C/X/V/Z/Y、Ctrl+Shift+Z；安全字段允许粘贴，但复制和剪切不导出内容。
- Home/End、Ctrl+方向键、Ctrl+Backspace/Delete、Shift 扩选；删除和粘贴后可以撤销。
- 鼠标拖选、Shift+点击扩选、双击选词；长文本光标移动时字段内跟随滚动。
- 单行字段粘贴多行文本时折叠换行；空剪贴板不会删除已选内容。
- Tab/Shift+Tab 切换字段；主机表单 Enter 校验并保存、Escape 取消；中文输入法选词时 Enter 不提前提交。
- 保持一个终端会话打开，再到 AI 服务商表单粘贴，内容不能进入终端。

真实客户端交互由人工验证；自动化回归通过不等同于完成此清单。
