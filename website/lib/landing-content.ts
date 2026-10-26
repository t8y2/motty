export type LandingContent = {
  badge: string;
  title: string;
  subtitle: string;
  start: string;
  install: string;
  heroAlt: string;
  featuresTitle: string;
  features: { icon: 'terminal' | 'key' | 'folder' | 'network' | 'monitor' | 'bot'; title: string; body: string }[];
  screensTitle: string;
  screens: { title: string; body: string; image: string; alt: string }[];
  principlesTitle: string;
  principles: { title: string; body: string }[];
  faqTitle: string;
  faq: { q: string; a: string }[];
  // The page's foot: who made it, on what, under what. Kept apart from the
  // license link so the line reads as two facts rather than one claim.
  footer: { copyright: string; platforms: string; license: string };
};

export const landing: Record<'en' | 'zh', LandingContent> = {
  en: {
    badge: '🎉 Native · one window: SSH · SFTP · VNC · RDP · MCP',
    title: 'SSH, in one native window.',
    subtitle:
      'Connect, move files, forward ports, remote desktop, ask the assistant — one window. Fast start, quiet at idle, MCP for outside agents.',
    start: 'Get started',
    install: 'Download',
    heroAlt: 'Motty with a terminal and the assistant panel',
    featuresTitle: 'Everything for a session',
    features: [
      {
        icon: 'terminal',
        title: 'A terminal that keeps up',
        body: 'Real PTY sessions, true colour, 5,000 lines of scrollback, inline images, splits, search and recording.',
      },
      {
        icon: 'key',
        title: 'Hosts and authentication',
        body: 'Host keys are verified before any credential is sent. Agent, key, password and one-time codes, with an encrypted vault.',
      },
      {
        icon: 'folder',
        title: 'Files without another app',
        body: 'SFTP with a resumable queue, atomic replacement and a built-in editor. SCP fallback, ZMODEM and XMODEM/YMODEM.',
      },
      {
        icon: 'network',
        title: 'Forwarding and jump chains',
        body: 'Local, remote, SOCKS5 and HTTP CONNECT rules. Every jump hop is verified on its own. X11 forwarding included.',
      },
      {
        icon: 'monitor',
        title: 'Remote desktops',
        body: 'VNC and RDP in a tab, with certificate pinning and an optional hop through SSH when the server only listens on loopback.',
      },
      {
        icon: 'bot',
        title: 'Assistant and MCP',
        body: 'Ask or Agent mode with a risk-classified approval gate. An optional, loopback-only MCP server for external agents.',
      },
    ],
    screensTitle: 'What it looks like running',
    screens: [
      {
        title: 'Hosts, groups and tags on one screen',
        body: 'Production, staging, bastions and databases each in their own group, tagged by environment. Type a name to connect; the icon follows the remote system, so a Windows box is obvious without labelling it.',
        image: '/screen-hosts.webp',
        alt: 'Motty\'s host list, tagged by environment',
      },
      {
        title: 'Files beside the terminal, not in another app',
        body: 'SFTP on the left, the shell on the right, same host and same session. Resumable transfers, atomic replacement and the editor live in the panel; a large upload keeps running in the queue without stalling the terminal.',
        image: '/screen-files.webp',
        alt: 'Motty\'s SFTP panel beside the terminal',
      },
      {
        title: 'Port forwards in the UI, not in a dotfile',
        body: 'Local, remote, SOCKS5 and HTTP CONNECT rules in one list: listener, target and state at a glance. Nothing listens until you press start.',
        image: '/screen-forward.webp',
        alt: 'Motty\'s port forwarding list',
      },
    ],
    principlesTitle: 'Principles',
    principles: [
      { title: 'Native by construction', body: 'Metal, Direct3D and Vulkan directly. Fast to start, nothing running while idle.' },
      { title: 'Built for the keyboard', body: 'Tabs, splits, SFTP and the assistant are palette commands. The mouse is optional.' },
      { title: 'Honest about limits', body: 'These docs say what was verified and what was not, including protocol gaps.' },
    ],
    faqTitle: 'Frequently asked questions',
    faq: [
      {
        q: 'Is Motty free?',
        a: 'Yes. Motty is open source under the Apache-2.0 license, and the packages for macOS, Windows and Linux cost nothing to download.',
      },
      {
        q: 'Which platforms does Motty run on?',
        a: 'macOS on Apple silicon and Intel, Windows on x64 and arm64, and Linux on x64 and arm64. Releases carry .dmg and .zip for macOS, an installer and .zip for Windows, and .deb and .tar.gz for Linux.',
      },
      {
        q: 'Does Motty use Electron?',
        a: 'No. It is a native application written in Go on the Viva desktop framework, drawing through Metal, Direct3D and Vulkan. There is no embedded browser and no JavaScript layer, and nothing runs while the window is idle.',
      },
      {
        q: 'What can Motty do beyond SSH?',
        a: 'SFTP and SCP with a resumable queue, ZMODEM, XMODEM and YMODEM, local, remote, SOCKS5 and HTTP CONNECT forwarding with X11, jump chains, serial, Telnet and local shells, and VNC and RDP desktops in a tab.',
      },
      {
        q: 'Can an AI agent use Motty?',
        a: 'Yes. Ask and Agent modes run inside the app behind a risk-classified approval gate, and an optional MCP server bound to loopback lets external agents drive your sessions.',
      },
    ],
    footer: {
      copyright: '© 2026 Motty',
      platforms: 'macOS · Windows · Linux',
      license: 'Apache-2.0 license',
    },
  },
  zh: {
    badge: '🎉 原生 · 一个窗口：SSH · SFTP · VNC · RDP · MCP',
    title: 'SSH，都在一个原生窗口里。',
    subtitle:
      '连接、传文件、转发端口、远程桌面、叫 AI 帮忙，都在一个界面里。原生渲染，启动即用、空闲不占资源，还能通过 MCP 交给外部 agent。',
    start: '快速开始',
    install: '下载',
    heroAlt: 'Motty 的终端与 AI 助手面板',
    featuresTitle: '一次会话所需的一切',
    features: [
      {
        icon: 'terminal',
        title: '跟得上的终端',
        body: '真正的 PTY 会话、真彩色、5000 行回滚、内联图片、分屏、搜索和录制。',
      },
      {
        icon: 'key',
        title: '主机与认证',
        body: '发送任何凭据之前先校验主机密钥。支持 agent、密钥、密码和一次性验证码，并有加密保险库。',
      },
      {
        icon: 'folder',
        title: '不用另一个文件工具',
        body: '带可续传队列的 SFTP、原子替换和内置编辑器。另有 SCP 回退、ZMODEM 与 XMODEM/YMODEM。',
      },
      {
        icon: 'network',
        title: '转发与跳板链',
        body: '本地、远程、SOCKS5 和 HTTP CONNECT 规则。每一跳独立校验，并支持 X11 转发。',
      },
      {
        icon: 'monitor',
        title: '远程桌面',
        body: '在标签页里使用 VNC 和 RDP，支持证书固定；服务器只监听回环时，可以经 SSH 中转。',
      },
      {
        icon: 'bot',
        title: '助手与 MCP',
        body: 'Ask 或 Agent 模式，命令按风险分级并需审批。可选的、仅回环的 MCP 服务器供外部代理使用。',
      },
    ],
    screensTitle: '实际跑起来是这样',
    screens: [
      {
        title: '主机、分组、标签，一屏之内',
        body: '生产、预发、跳板、数据库各自成组，标签按环境打。搜索框里敲主机名直接连，图标按远端系统自动认——Windows 机器不用自己标。',
        image: '/screen-hosts.webp',
        alt: 'Motty 的主机列表，按环境打标签',
      },
      {
        title: '文件就在终端旁边，不用另开客户端',
        body: '左边 SFTP，右边 shell，同一台主机、同一个会话。断点续传、冲突检查后的原子保存、内置编辑器都在面板里；大文件在队列里跑，终端不卡。',
        image: '/screen-files.webp',
        alt: 'Motty 的 SFTP 面板与终端并排',
      },
      {
        title: '端口转发在界面里，不在 dotfile 里',
        body: '本地、远程、SOCKS5、HTTP CONNECT 四种规则一个列表：监听地址、目标地址、状态一眼看到。默认不监听，点启动才建立连接。',
        image: '/screen-forward.webp',
        alt: 'Motty 的端口转发列表',
      },
    ],
    principlesTitle: '原则',
    principles: [
      { title: '原生，不是网页', body: '直接调用 Metal、Direct3D 和 Vulkan。启动快，空闲时不空转。' },
      { title: '为键盘而生', body: '标签页、分屏、SFTP 和助手都是命令面板里的命令，鼠标是可选的。' },
      { title: '如实说明限制', body: '文档会写明哪些已验证、哪些没有，包括协议缺口。' },
    ],
    faqTitle: '常见问题',
    faq: [
      {
        q: 'Motty 是免费的吗？',
        a: '是。Motty 以 Apache-2.0 许可开源，macOS、Windows 和 Linux 的安装包都可以免费下载。',
      },
      {
        q: 'Motty 支持哪些平台？',
        a: '支持 Apple 芯片与 Intel 的 macOS、x64 与 arm64 的 Windows，以及 x64 与 arm64 的 Linux。发布包提供 .dmg 与 .zip、安装程序与 .zip、.deb 与 .tar.gz。',
      },
      {
        q: 'Motty 用 Electron 吗？',
        a: '不用。它是 Go 编写的原生应用，基于 Viva 桌面框架，直接调用 Metal、Direct3D 和 Vulkan；没有内嵌浏览器，也没有 JavaScript 层，窗口空闲时不运行任何东西。',
      },
      {
        q: '除了 SSH，Motty 还能做什么？',
        a: 'SFTP 与 SCP（带可续传队列）、ZMODEM/XMODEM/YMODEM、本地/远程/SOCKS5/HTTP CONNECT 转发与 X11、跳板链、串口、Telnet 与本地 Shell，以及标签页里的 VNC 与 RDP 远程桌面。',
      },
      {
        q: 'AI agent 能用 Motty 吗？',
        a: '可以。应用内的 Ask 与 Agent 模式按风险分级审批，可选的 MCP 服务器仅监听回环，让外部 agent 驱动你的会话。',
      },
    ],
    footer: {
      copyright: '© 2026 Motty',
      platforms: 'macOS · Windows · Linux',
      license: 'Apache-2.0 许可',
    },
  },
};
