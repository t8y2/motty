/**
 * The changelog, newest first: one entry per release, written when the release
 * is published (docs/release.md). Both languages live next to each other so a
 * release does not need a separate docs translation pass, and the page stays a
 * static export — nothing is fetched at build or run time.
 */
export type ChangelogText = { en: string; zh: string };

export type ChangelogSection = {
  title: ChangelogText;
  items: ChangelogText[];
};

export type ChangelogEntry = {
  /** Version without the v; the release tag is `v${version}`. */
  version: string;
  /** Publish date, ISO 8601. */
  date: string;
  sections: ChangelogSection[];
};

export const changelog: ChangelogEntry[] = [
  {
    version: '0.1.4',
    date: '2026-10-28',
    sections: [
      {
        title: { en: 'New', zh: '新功能' },
        items: [
          {
            en: '`motty mcp-serve` is an MCP server over stdio: an agent starts it itself, with no port and no token, and it ends with that agent. It reaches only the hosts you opened to agents under a host’s Advanced settings.',
            zh: '`motty mcp-serve` 是走 stdio 的 MCP 服务：agent 自己启动它，不用端口也不用 token，agent 退出它就结束；它只访问你在主机「高级」里开放给 agent 的主机。',
          },
          {
            en: 'Agents are read only by default; the Commands mode also allows commands rated low risk. Anything that needs approval is refused, host keys are never trusted by the server itself, and passwords come from the saved passwords.',
            zh: 'agent 默认只读，「命令」档额外放行低风险命令；需要确认的操作一律拒绝，服务不会自行信任主机密钥，密码取自已保存的密码。',
          },
          {
            en: 'Every call is listed under Settings > MCP for 30 days, without its output; the server refuses to start when it cannot open that log. Settings > MCP also carries copy-ready entries for the supported clients.',
            zh: '每次调用都会列在设置 > MCP，保留 30 天且不含输出，日志打不开时服务拒绝启动；设置 > MCP 里还有可直接复制的客户端配置片段。',
          },
          {
            en: 'A GitHub button in the window bar opens the project page. The language button is gone; the language still lives in Settings.',
            zh: '窗口栏新增 GitHub 按钮，点开是项目主页；语言按钮去掉，语言仍在设置里选。',
          },
        ],
      },
      {
        title: { en: 'Improved', zh: '改进' },
        items: [
          {
            en: 'Advanced host settings are grouped under Session and AI agents, name and color share a row, the agent switch explains itself in a help tooltip, and the proxy summary is a bordered box like the other inputs.',
            zh: '主机高级设置按「会话」和「AI agent」分组，名称和颜色排在同一行，agent 开关带帮助提示，代理摘要改成和其他输入一致的有边框盒子。',
          },
          {
            en: 'iTerm2, kitty and sixel images decode in the background, so a large image no longer stalls a frame; the placement is reserved while decoding, and erases, resizes and kitty deletes still see it.',
            zh: 'iTerm2、kitty、sixel 图片改到后台解码，大图不再卡住一帧；解码期间位置已经占好，擦除、缩放和 kitty 删除照常生效。',
          },
          {
            en: 'Hovering a terminal link scans only the soft-wrapped group under the pointer instead of the whole scrollback, and saving one host re-encodes only that host’s record.',
            zh: '悬停终端链接只扫描指针所在的那一组软换行，不再整屏回扫；保存一台主机只重新编码那条记录，主机很多时也快。',
          },
        ],
      },
      {
        title: { en: 'Fixed', zh: '修复' },
        items: [
          {
            en: 'The MCP group in Settings no longer collides with the cards around it, and its title and descriptions say what the user cares about.',
            zh: '设置里的 MCP 分组不再和周围的卡片挤在一起，标题和说明改成用户关心的说法。',
          },
        ],
      },
    ],
  },
  {
    version: '0.1.3',
    date: '2026-10-27',
    sections: [
      {
        title: { en: 'New', zh: '新功能' },
        items: [
          {
            en: 'SFTP opens in its own window with the local and remote panes side by side; rows sort, drop onto each other, and a pane can connect straight to a host.',
            zh: 'SFTP 可以在自己的窗口里并排打开本地和远程两栏，行可排序、互相拖放，也能从主机直连。',
          },
          {
            en: 'A global transfer center lists every upload and download with its progress and lifetime, and keeps going after the window that started it closes. The file panel shows the transfer list by default and docks left or right, and its path bar is editable with a choice of style.',
            zh: '全局传输中心列出每个上传下载的进度和生命周期，发起它的窗口关掉后任务照常走完。文件面板默认显示传输列表，可停靠左侧或右侧，路径栏可编辑并能切换样式。',
          },
          {
            en: 'Compress a remote folder into a zip on the server itself, and extract more archive formats with a notice of how it went.',
            zh: '远程文件夹直接在服务器上打包成 zip，解压支持更多归档格式，并给出结果提示。',
          },
          {
            en: 'Preview images and edit text in a native window: the encoding is detected, and the main window stays free.',
            zh: '图片在独立窗口里预览，文本按识别出的编码编辑，主窗口不被占用。',
          },
          {
            en: 'Remote tools open in their own native window and run alongside the main window; legacy Docker and ps output is understood, and a container gets its own terminal.',
            zh: '远程工具开在自己的原生窗口里，和主窗口并行；兼容旧版 Docker 和 ps 的输出，进入容器时用它自己的终端。',
          },
          {
            en: 'Each host can choose its own authentication methods and its own SOCKS5 or HTTP CONNECT proxy, credentials included.',
            zh: '每台主机可以单独选择认证方式，也可以单独配 SOCKS5 或 HTTP CONNECT 代理，含用户名密码。',
          },
          {
            en: 'A connection can carry its own color into the terminal header and tool buttons, picked with a round eyedropper in the form; the host form rows got roomier.',
            zh: '每个连接可以带一种颜色，出现在终端顶栏和工具按钮上，表单里用圆形吸管取色；主机表单的行距也调宽了。',
          },
          {
            en: 'Host tags are a searchable multi-select that can create an entry on the spot.',
            zh: '主机标签改成可搜索、可现场新建条目的多选框。',
          },
          {
            en: 'Import model providers from magpie and cc-switch, drawn with their official marks.',
            zh: '可以从 magpie 和 cc-switch 导入模型供应商，并使用它们的官方标识。',
          },
          {
            en: 'Close a split pane from the terminal menu and the keyboard follows the pane you enter; the command palette keeps the highlighted row under the pointer and takes arrow keys in its search field.',
            zh: '终端菜单里可以关闭分屏，键盘跟着你进入的分栏走；命令面板在鼠标悬浮时保留选中项，搜索框里可以用方向键移动。',
          },
        ],
      },
      {
        title: { en: 'Improved', zh: '改进' },
        items: [
          {
            en: 'Every pull request runs the full Go checks, and the race detector is sharded with cached builds so the gate finishes sooner.',
            zh: '每个 PR 都跑完整的 Go 检查，race 检测拆成分片并缓存构建，门禁更快也更稳。',
          },
          { en: 'The debug gallery gained a color swatch.', zh: '调试画廊里加了颜色样本。' },
        ],
      },
      {
        title: { en: 'Fixed', zh: '修复' },
        items: [
          {
            en: 'The local file panel no longer reports an SFTP EOF when it enters an empty directory.',
            zh: '本地文件面板进入空目录时不再误报 SFTP EOF。',
          },
          {
            en: 'A restored tab starts disconnected instead of reconnecting on its own.',
            zh: '恢复的标签页默认不再自动重连。',
          },
          { en: 'Set Model in the AI panel opens the AI settings page.', zh: 'AI 面板里的「设置模型」跳到 AI 设置页。' },
          {
            en: 'The command palette, overlay close buttons and the new-tab picker take their clicks and keys where they are drawn.',
            zh: '命令面板、overlay 关闭按钮和新标签页选择器的点击与按键落在它画出来的位置。',
          },
          {
            en: 'An RDP server can no longer make the client hold unbounded pixel memory.',
            zh: 'RDP 服务端不能再让客户端持有无上限的像素内存。',
          },
          {
            en: 'The macOS update helper runs from a full copy of the bundle, so the replacement lands reliably.',
            zh: 'macOS 更新助手从完整的 bundle 副本运行，替换更可靠。',
          },
        ],
      },
    ],
  },
  {
    version: '0.1.2',
    date: '2026-10-27',
    sections: [
      {
        title: { en: 'New', zh: '新功能' },
        items: [
          {
            en: 'An About page in Settings shows this copy\'s version, platform, build commit and runtime, with one button that copies them for a bug report, and links to the community, the repository, the issue tracker and motty.page.',
            zh: '设置里新增「关于」，显示这一份 Motty 的版本、平台、构建 commit 和运行时，一个按钮把它们复制下来贴进 issue，并提供社区、仓库、issue 和 motty.page 的入口。',
          },
          {
            en: 'Hosts now sync both ways with ~/.ssh/config: host, user, port and ProxyJump travel either direction and IdentityFile lands in Keys. The file keeps its comments and unknown directives, a block naming several hosts is left alone, and when both sides changed the file wins.',
            zh: '主机与 ~/.ssh/config 双向同步：host、user、port、ProxyJump 两个方向都同步，IdentityFile 落到 Keys。文件保留注释和未知指令，一条 Host 行带多个主机名的块不动，两边都改时以文件为准。',
          },
          {
            en: 'Select text inside an AI answer and copy it; a fenced block keeps its own line breaks and indentation.',
            zh: 'AI 回答里的文字可以选中并复制，代码块保留原有的换行和缩进。',
          },
          {
            en: 'Command/Control+Shift+T opens a local terminal.',
            zh: 'Command/Control+Shift+T 打开本地终端。',
          },
          { en: 'Check for Updates… in the status-area menu.', zh: '状态栏菜单里的「检查更新…」。' },
        ],
      },
      {
        title: { en: 'Improved', zh: '改进' },
        items: [
          {
            en: 'Updates are checked five seconds after a window wakes and then once an hour, on by default.',
            zh: '窗口唤醒 5 秒后检查一次更新，之后每小时一次，默认开启。',
          },
          {
            en: 'An install the system owns (a distribution package, /snap, Flatpak, Program Files) is told to update through its package manager or the new installer, and is still never replaced by the app with elevation.',
            zh: '系统持有的安装（发行版包、/snap、Flatpak、Program Files）会被告知用包管理器或新安装包更新，应用仍然不会提权替换它。',
          },
          {
            en: 'Every package carries a minisign update signature that names its version and file, and the app verifies it before replacing anything. Windows can now update in place without a code-signing certificate; macOS still checks the Developer ID team.',
            zh: '每个包都带 minisign 更新签名，签名里写明版本和文件名，应用在替换任何东西之前先校验。Windows 不再需要代码签名证书也能原地更新；macOS 仍校验 Developer ID 团队。',
          },
          {
            en: 'The component gallery is a development-only surface and no longer appears in release builds.',
            zh: '组件画廊只在开发构建里出现，发布构建不再包含它。',
          },
        ],
      },
      {
        title: { en: 'Fixed', zh: '修复' },
        items: [
          {
            en: 'A UTF-8 control string could leak its continuation bytes into the input line as text. Raw eight-bit C1 DCS/ST/OSC/APC are no longer interpreted as controls; use their seven-bit ESC forms.',
            zh: 'UTF-8 控制字符串里的续字节此前会漏进输入行变成文字。原始的八位 C1 DCS/ST/OSC/APC 现在不再被当作控制符解释，请改用七位 ESC 形式。',
          },
          { en: 'The MCP configuration preview names itself in its header.', zh: 'MCP 配置预览的头部写明它的名称。' },
        ],
      },
    ],
  },
  {
    version: '0.1.1',
    date: '2026-10-26',
    sections: [
      {
        title: { en: 'New', zh: '新功能' },
        items: [
          {
            en: 'Motty updates itself: it downloads the release from the mirror, checks its SHA-256 and signature, and restarts into the new version. Installs it does not own (a .deb, Program Files, a portable copy) still open the download page.',
            zh: 'Motty 可以自己更新了：从镜像下载安装包，校验 SHA-256 和签名后重启进入新版本。不是自己持有的安装（.deb、Program Files、便携版）仍会打开下载页。',
          },
          {
            en: 'Local Claude Code, Codex and OpenCode CLI providers, each with its own login terminal.',
            zh: '本地 Claude Code、Codex、OpenCode CLI 提供方，各自带登录终端。',
          },
          {
            en: 'A per-host outbound proxy: inherit, direct, SOCKS5 or HTTP CONNECT.',
            zh: '每台主机可单独设置出站代理：继承、直连、SOCKS5 或 HTTP CONNECT。',
          },
          {
            en: 'Copy the MCP client configuration for 13 clients from Settings.',
            zh: '在设置里复制 13 种客户端的 MCP 配置。',
          },
          { en: 'Import Electerm bookmarks with their nested groups.', zh: '导入 Electerm 书签及其嵌套分组。' },
          {
            en: 'Save a host password when creating or copying a host; hosts with the same endpoint can share it.',
            zh: '新建或复制主机时保存密码，同一地址的主机可以共用。',
          },
          { en: 'Duplicate a tab and close a pane from the interface.', zh: '界面里复制标签页、关闭分屏。' },
        ],
      },
      {
        title: { en: 'Fixed', zh: '修复' },
        items: [
          { en: 'Ctrl+C reaches a terminal without a selection again.', zh: '终端没有选中内容时也能收到 Ctrl+C。' },
          {
            en: 'AI rename dialogs take their buttons again, and the wheel no longer scrolls the panel behind them.',
            zh: 'AI 重命名对话框的按钮恢复可点，滚轮不再滚动背后的面板。',
          },
          {
            en: 'The host form keeps its fields on one grid in a narrow window.',
            zh: '窄窗口下主机表单的字段保持在同一栅格。',
          },
        ],
      },
    ],
  },
  {
    version: '0.1.0',
    date: '2026-10-26',
    sections: [
      {
        title: { en: 'New', zh: '新功能' },
        items: [
          { en: 'Native SSH terminal with tabs and split panes.', zh: '原生 SSH 终端，支持标签页和分屏。' },
          {
            en: 'Hosts, groups and tags, with password or key authentication and jump chains.',
            zh: '主机、分组、标签，支持密码或密钥认证以及跳板链。',
          },
          {
            en: 'An SFTP browser beside the terminal: upload, download, rename and delete.',
            zh: '终端旁的 SFTP 文件浏览：上传、下载、重命名、删除。',
          },
          {
            en: 'Port forwarding managed in the interface: local, remote and dynamic.',
            zh: '界面里管理端口转发：本地、远程、动态。',
          },
          { en: 'VNC and RDP desktops in the same window.', zh: '同一个窗口里的 VNC 和 RDP 桌面。' },
          {
            en: 'An AI assistant and an MCP server that work on the session in front of you.',
            zh: 'AI 助手与 MCP 服务，作用于当前会话。',
          },
        ],
      },
      {
        title: { en: 'Packaging', zh: '安装' },
        items: [
          {
            en: 'Packages for macOS (arm64, amd64), Windows (amd64, arm64) and Linux (amd64, arm64). The macOS builds are signed, notarized and stapled; the Windows and Linux builds are unsigned.',
            zh: 'macOS（arm64、amd64）、Windows（amd64、arm64）、Linux（amd64、arm64）安装包。macOS 已签名、公证并 staple；Windows 和 Linux 尚未签名。',
          },
        ],
      },
    ],
  },
];

export const changelogPage: Record<
  'en' | 'zh',
  {
    title: string;
    description: string;
    versions: string;
    publishedOn: string;
    download: string;
    tocSubtitle: string;
    /** `In v0.1.0` above the section list of the release in view. */
    tocTitle: (version: string) => string;
  }
> = {
  en: {
    title: 'Changelog',
    description: 'Track every release: features, improvements and fixes.',
    versions: 'Versions',
    publishedOn: 'Published on',
    download: 'Download',
    tocSubtitle: 'Release contents',
    tocTitle: (version) => `In ${version}`,
  },
  zh: {
    title: '更新日志',
    description: '追踪每次发布：新功能、改进和修复。',
    versions: '版本列表',
    publishedOn: '发布于',
    download: '下载',
    tocSubtitle: '更新内容',
    tocTitle: (version) => `在 ${version} 中`,
  },
};

/** `v0.1.0`, the way the tag and the release title spell it. */
export function versionLabel(version: string): string {
  return `v${version}`;
}

/** Anchor ids in the same shape the DBX changelog uses. */
export function releaseId(version: string): string {
  return `changelog-release-v${version.replace(/\./g, '-')}`;
}

export function sectionId(version: string, index: number): string {
  return `${releaseId(version)}-section-${index}`;
}
