# Motty 的 .deb 打包指南

本文依据本仓库的 `viva.toml`、`go.mod`、`tools/release.sh` 和构建文档整理，适用于 Linux 上本地打包。

## 1. 打包方式与当前环境

项目通过 **Viva CLI 的 `package` 命令**生成 Debian 安装包，Makefile 没有 `deb` 打包目标。

2026-11-18 检查结果：

- 项目目录：`/data/workspace/ai/motty`；机器架构：`x86_64`，对应打包参数 `amd64`。
- `viva.toml` 当前版本是 `0.4.14`，默认输出目录为 `build/dist`。
- `go.mod` 要求 Go **1.26.11**；本地已升级至 **1.26.11**，安装目录为 `/soft/dev/go`。
- 升级时已校验官方安装包 SHA-256；旧版保留在 `/soft/dev/go.backup-1.26.3-20261118-152658`。
- Rust、Cargo、Git、`dpkg-deb` 已安装。此项目的打包入口是 Go 命令，无需直接执行 `cargo build`。
- 当前用户存在 `~/.bashrc`，未发现用户提及的 `~/.bash_rc`。
- 缺少 `/data/workspace/ai/viva`。`go.mod` 的本地替换要求 Viva 位于 `../viva`。

本次已核对配置及脚本，尚未执行完整构建；以下产物名称为仓库定义的预期名称。

## 2. 加载环境并检查工具链

在 Bash 交互终端执行；环境文件只在本机加载，无需输出其内容：

```bash
if [ -f "$HOME/.bash_rc" ]; then
  source "$HOME/.bash_rc"
else
  source "$HOME/.bashrc"
fi
cd /data/workspace/ai/motty
command -v go rustc cargo git dpkg-deb
GOTOOLCHAIN=local go version
```

`.bashrc` 可能在非交互 Shell 中提前返回；自动化执行可使用 `bash -ic '命令'`。
上面的 `GOTOOLCHAIN=local` 仅用于查看已安装版本，不应作为低版本 Go 的构建配置。

在项目目录确认实际构建工具链：

```bash
go version
```

该命令可能触发 Go 自动下载工具链。如果长时间无响应，检查工具链下载网络；
如果提示 Go 版本不足，安装 Go 1.26.11 或兼容的更新版本，并在环境文件中更新 PATH。
同时需满足所检出 Viva 版本自身 `go.mod` 的要求，不建议通过降低项目版本声明绕过检查。

## 3. 准备同级 Viva 仓库

仅在 `../viva` 不存在时执行克隆：

```bash
cd /data/workspace/ai
git clone https://github.com/vivaui/viva.git viva
cd motty
test -f ../viva/go.mod
git -C ../viva rev-parse HEAD
```

目录关系必须是 `ai/motty` 与 `ai/viva` 同级，因为项目使用：
`replace github.com/vivaui/viva => ../viva`。
Viva 是独立项目，需要遵守其许可证；若仓库访问受限，需先取得访问权限。
正式交付应记录 Motty 和 Viva 的 commit，以便复现；不要假设未来的 Viva main 始终兼容。

## 4. 打包当前工作区（推荐本地使用）

生成 amd64 安装包：

```bash
cd /data/workspace/ai/viva
go run ./cmd/viva package -C ../motty --target linux --arch amd64 --format deb
```

需要同时生成便携压缩包时，将 `--format deb` 改为 `--format tar.gz,deb`。
ARM64 目标使用 `--arch arm64`；应在对应架构的机器上验证运行效果。
首次执行可能需要下载 Go 模块及 Viva 所需资源，需保证依赖下载网络可用。

按当前配置，amd64 产物预期为：

```text
/data/workspace/ai/motty/build/dist/motty_0.4.14_amd64.deb
```

版本号来自 `viva.toml` 的 `[app].version`，输出目录来自 `[build].out`。
如需自定义输出目录，可为 `package` 添加 `-o /绝对路径/输出目录`。
此方式包含当前工作区修改；不要添加 `dev` 或 `vivahead` 标签来制作普通桌面安装包。

## 5. 检查与安装

以下命令在 Motty 根目录运行，动态读取版本号，避免升级版本后仍使用旧文件名：

```bash
cd /data/workspace/ai/motty
version=$(sed -n 's/^version *= *"\([^"]*\)".*/\1/p' viva.toml | head -1)
deb="build/dist/motty_${version}_amd64.deb"
test -f "$deb"
dpkg-deb --info "$deb"
dpkg-deb --contents "$deb"
sha256sum "$deb"
```

检查包的 Package、Version、Architecture、Depends 及文件清单。
在与安装包架构匹配的 Debian/Ubuntu 桌面环境中安装：

```bash
sudo apt install "./$deb"
dpkg-query -W motty
motty
```

`apt install` 会根据包的依赖声明解析运行时依赖。桌面运行需要 Linux 图形环境，
仓库说明要求 X11；默认图形后端为 XCB + Vulkan。无显示服务的服务器上打包与启动 GUI 是不同步骤。
若提示缺少共享库或无法初始化图形后端，结合实际错误检查图形环境及驱动。

## 6. 按已提交版本生成 Linux 发布产物

需要与项目发布脚本一致的流程时，在 Motty 根目录执行：

```bash
cd /data/workspace/ai/motty
viva_commit=$(git -C ../viva rev-parse HEAD)
bash tools/release.sh --platforms linux --ref HEAD --viva-ref "$viva_commit"
```

该脚本从指定 commit 创建临时干净检出，不包含未提交修改；同时生成 amd64、arm64 的
`.deb`、`.tar.gz` 和 Arch Linux 打包描述文件，并检查二进制构建信息。
默认输出为 `build/release-<版本>/`，可用 `--out /绝对路径/输出目录` 更改。
仅构建 Linux 时不会走完整平台矩阵的 SHA256SUMS 生成分支，可自行用 `sha256sum` 校验。
上述命令没有 `--upload`，不会发布到 GitHub；本地生成 `.deb` 不需要配置发布签名密钥。

## 7. 常见问题

| 现象 | 处理方式 |
| --- | --- |
| `go: command not found` | 加载实际存在的环境文件；非交互 Shell 注意 `.bashrc` 的提前返回逻辑。 |
| Go 提示版本不足或下载工具链停滞 | 满足 Motty 与 Viva 的 Go 版本要求，检查工具链下载网络。 |
| 找不到 `../viva/go.mod` | 将 Viva 检出到 Motty 的同级 `viva` 目录。 |
| Viva CLI 或 Go 依赖下载失败 | 检查仓库权限和依赖网络；确认 Viva commit 与项目兼容。 |
| 安装包架构不匹配 | `x86_64` 使用 `amd64`；`aarch64` 使用 `arm64`。 |
| 本地修改没有进入发布包 | `release.sh` 使用 commit；打包当前工作区请使用第 4 节。 |

依据：[打包配置](../viva.toml)、[Go 模块](../go.mod)、[发布脚本](../tools/release.sh)、
[从源码构建](../website/content/docs/development/build-from-source.zh.mdx)。
