# 界面语言与资源维护

Motty 的语言列表与 DBX 保持一致，共 14 种。显示顺序为简体中文、繁体中文、
English，再按 DBX 原顺序显示其余语言。列表、原生名称、资源标识和系统语言
匹配集中在 `internal/i18n/languages.go`；新增显示顺序不会改变已有的 Language
枚举值或保存的语言标记。

英语和简体中文使用 Go 源文案，其余 12 种语言使用内置的完整 JSON 目录。
每种语言覆盖当前 Messages 的全部 1,751 个文本字段；额外目录仅在首次选择
时解码并缓存，切换语言和启动应用不请求翻译服务。

## 更新翻译

导出两份完整源目录：

```sh
go run ./tools/i18n-catalog -out source-catalogs.json
```

增加或修改 Messages 字段时，同步更新 `internal/i18n/locales/`。保持 printf
参数、快捷键、代码片段与产品名称完整；参数需要调整顺序时使用 Go 的显式
参数索引，例如 `%[2]s`。运行以下检查以发现缺失文案和参数不一致：

```sh
go test ./internal/i18n
```

手动选择的语言以标准目录标记保存，重启后优先恢复。没有明确选择时才匹配
系统偏好语言，未支持的语言使用英语。

## 图标资源

`internal/workbench/flags/svg/` 保存从 DBX 直接复制的 13 个原始 SVG，以及
对应的许可证与来源说明。繁体中文使用与 DBX 一致的“繁”字标识，没有旗帜资源。

为保留 SVG 引用、marker、描边、裁剪和填充规则，维护工具通过完整的浏览器
SVG 渲染器生成 128 × 96 的内置 PNG；不会在运行时使用不完整的 SVG 子集解析器。
修改 SVG 后在已安装 Chrome 的环境运行：

```sh
cd tools/flags
npm ci
npm run generate
```

可通过 `CHROME_PATH` 指定浏览器。生成结果与源文件的 SHA-256 写入 manifest，
测试会检查资源是否同步。正常构建、分发和运行 Motty 不需要 Node.js 或 Chrome。

## Viva 配套

[Viva PR #50](https://github.com/vivaui/viva/pull/50) 提供通用的 Select 图片与
文字前缀，并修复虚拟化列表在 Home/End 跨可视区域跳转时的待解析焦点展示。
Motty 使用公共 `IconImage`、`IconText` 接口，语言业务状态仍由应用持有。

本轮框架验证版本：`0c7d7622b82f88049465f10b9091edb7c62c8ce3`。
CI 默认检出 Viva main，因此新接口需要该配套 PR 合入后才能在 CI 使用。
