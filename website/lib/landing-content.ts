export type ShowcaseContent = {
  base: { image: string; alt: string };
  steps: { image: string; title: string; body: string }[];
};

export type LandingContent = {
  badge: string;
  title: string;
  subtitle: string;
  start: string;
  install: string;
  showcase: ShowcaseContent;
  featuresTitle: string;
  features: { icon: 'terminal' | 'key' | 'folder' | 'network' | 'monitor' | 'bot'; title: string; body: string }[];
  screensTitle: string;
  screens: { title: string; body: string; image: string; alt: string }[];
  faqTitle: string;
  /**
   * The answer-shaped paragraph: one self-contained block of 120-160 words that
   * names the product, the category, the licence and the differentiators, so an
   * engine can quote it without stitching two pages together.
   */
  aboutTitle: string;
  about: string;
  /**
   * Replies from real users of the launch thread. `post` is the reply's number
   * in that topic, kept as a stable key and so a quote can be traced back to
   * its source; `avatar` is a downloaded copy of the forum avatar.
   */
  testimonialsTitle: string;
  testimonials: { quote: string; author: string; avatar: string; post: number }[];
  faq: { q: string; a: string }[];
  // The page's foot: who made it, on what, under what. Kept apart from the
  // license link so the line reads as two facts rather than one claim.
  footer: { copyright: string; platforms: string; license: string };
};

type TestimonialSource = {
  post: number;
  author: string;
  avatar: string;
  zh: string;
  en: string;
};

// One entry per person; the quote is the only part that changes with language,
// so the avatar and the traceable reply number are not duplicated per locale.
const testimonialSources: TestimonialSource[] = [
  {
    post: 49,
    author: 'onlyxx',
    avatar: '/avatars/onlyxx.webp',
    zh: '刚刚体验了，启动太丝滑了，秒起都不是，是眨眼起。',
    en: 'Just tried it: startup is not merely fast — you blink and it is there.',
  },
  {
    post: 14,
    author: 'lizhiangg',
    avatar: '/avatars/lizhiangg.webp',
    zh: 'Beta 版完成度惊人，做本地终端软件也足够了。',
    en: 'Astonishingly complete for a beta — already enough as a local terminal.',
  },
  {
    post: 244,
    author: 'usr',
    avatar: '/avatars/usr.webp',
    zh: '看了图和介绍，火速下载试用，是我想要的。……我能想到的基本都有了，且长得好看、也不算臃肿。',
    en: 'Saw the screenshots and the description and downloaded it right away — exactly what I wanted. … Everything I can think of is there, it looks good, and it is not bloated.',
  },
  {
    post: 130,
    author: 'amminxing',
    avatar: '/avatars/amminxing.webp',
    zh: '已体验，非常的丝滑，感谢大佬开源。',
    en: 'Tried it — very smooth. Thanks for open-sourcing it.',
  },
  {
    post: 291,
    author: 'Wolfsoft',
    avatar: '/avatars/wolfsoft.webp',
    zh: '这终端是真好用，我现在连我那堆服务器基本都用这个了。',
    en: 'This terminal is genuinely good — I use it for basically all my servers now.',
  },
  {
    post: 184,
    author: 'Isaacwen12138',
    avatar: '/avatars/isaacwen12138.webp',
    zh: 'Motty 是真的好用，而且更新速度也很惊人啊。',
    en: 'Motty is genuinely good, and the update pace is astonishing.',
  },
  {
    post: 297,
    author: 'zedcoyo',
    avatar: '/avatars/zedcoyo.webp',
    zh: '目前这个 Motty 挺好用的，资源占用也低。',
    en: 'Motty works well for me, and it uses very little memory.',
  },
  {
    post: 44,
    author: 'ReiGao',
    avatar: '/avatars/lingborei.webp',
    zh: '用上了 Motty，用起来很丝滑，喜欢，准备长期使用。',
    en: 'Been using Motty and it feels very smooth. I like it, and I plan to keep it.',
  },
  {
    post: 63,
    author: 'timnottom',
    avatar: '/avatars/timnottom.webp',
    zh: '我去我去，爱了爱了。',
    en: 'Oh wow — I love it.',
  },
  {
    post: 246,
    author: 'superj',
    avatar: '/avatars/superj.webp',
    zh: '已经体验了好几天了，佬的更新速度惊人……用起来丝滑不卡顿。',
    en: 'Used it for days now — the update pace is remarkable — and it runs smoothly, without stutter.',
  },
  {
    post: 284,
    author: 'yiii123',
    avatar: '/avatars/yiii123.webp',
    zh: '一周前下了，重度使用了几天，非常好用。',
    en: 'Downloaded it a week ago and used it heavily for days — very good.',
  },
  {
    post: 178,
    author: 'ioaicwj',
    avatar: '/avatars/ioaicwj.webp',
    zh: '这几天用过，很不错。',
    en: 'Used it over the past few days — very good.',
  },
  {
    post: 133,
    author: 'whq',
    avatar: '/avatars/whq.webp',
    zh: '真是好东西呀，已经标星了。',
    en: 'A genuinely good thing — starred it already.',
  },
  {
    post: 227,
    author: 'MWBDTTH',
    avatar: '/avatars/mwbdtth.webp',
    zh: '已严肃使用，使用一段时间看看怎么样，目前整体感觉挺好的。',
    en: 'Gave it a serious try for a while — so far it feels good overall.',
  },
];

function pickTestimonials(lang: 'en' | 'zh') {
  return testimonialSources.map((t) => ({
    post: t.post,
    author: t.author,
    avatar: t.avatar,
    quote: lang === 'zh' ? t.zh : t.en,
  }));
}

export const landing: Record<'en' | 'zh', LandingContent> = {
  en: {
    badge: '🎉 Native · one window: SSH · SFTP · VNC · RDP · MCP',
    title: 'SSH, in one native window.',
    subtitle:
      'Connect, move files, forward ports, remote desktop, ask the assistant — one window. Fast start, quiet at idle, MCP for outside agents.',
    start: 'Get started',
    install: 'Download',
    showcase: {
      base: {
        image: '/showcase-base-dark.webp',
        alt: 'Motty with the file panel, terminal and assistant open on the staging-web host',
      },
      steps: [
        {
          image: '/showcase-light.webp',
          title: 'Light or dark, in one click',
          body: 'The theme button in the title bar switches the whole window; the session keeps running.',
        },
        {
          image: '/showcase-hosts.webp',
          title: 'Every host, grouped and tagged',
          body: 'Production, staging and CI in one list, tagged by environment; type a name to connect.',
        },
        {
          image: '/showcase-files.webp',
          title: 'Files beside the shell',
          body: 'SFTP and a terminal on the same session, with a resumable queue and an editor in the panel.',
        },
        {
          image: '/showcase-colors.webp',
          title: 'More than 200 colour schemes',
          body: 'Tokyo Night, Dracula, Nord and the rest, with one scheme for dark and one for light.',
        },
        {
          image: '/showcase-light.webp',
          title: 'An assistant on the same session',
          body: "It reads this machine's logs and processes, and tool calls go through an approval gate.",
        },
      ],
    },
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
        body: 'SFTP with a resumable queue, atomic replacement and a built-in editor. SCP fallback, ZMODEM, XMODEM/YMODEM and TRZSZ.',
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
    aboutTitle: 'What Motty is',
    about:
      'Motty is a free, open-source SSH client for macOS, Windows and Linux that puts a terminal, a file panel, port forwarding, remote desktops and an AI assistant in one native window. It is written in Go on the Viva desktop framework, so there is no Electron runtime and no embedded browser: the window draws through Metal, Direct3D or Vulkan, starts in under a second, and schedules nothing while it sits idle. Host keys are verified against Motty\'s own known_hosts before any credential is sent, and the vault is AES-256-GCM with its key in the system secret store. The same window carries SFTP and SCP with a resumable queue, local, remote, SOCKS5 and HTTP CONNECT forwarding with X11, jump chains that verify every hop, and VNC and RDP tabs whose certificates can be pinned.',
    testimonialsTitle: 'What users say',
    testimonials: pickTestimonials('en'),
    faqTitle: 'Frequently asked questions',
    // Answers run 40-60 words: long enough to be a self-contained citation, short
    // enough that an answer engine can lift one without trimming it. Keep that
    // length when editing — it is the whole reason these are worded this way.
    faq: [
      {
        q: 'Is Motty free?',
        a: 'Yes. Motty is open source under the Apache-2.0 license, and the packages for macOS, Windows and Linux cost nothing to download. There is no account, no trial period and no paid tier, and no analytics or telemetry library is linked into the application.',
      },
      {
        q: 'Which platforms does Motty run on?',
        a: 'macOS on Apple silicon and Intel, Windows on x64 and arm64, and Linux on x64 and arm64. Every release ships a .dmg and a .zip for macOS, an installer and a .zip for Windows, and .deb and .tar.gz packages for Linux.',
      },
      {
        q: 'Does Motty use Electron?',
        a: 'No. Motty is a native application written in Go on the Viva desktop framework, drawing through Metal, Direct3D and Vulkan. There is no embedded browser and no JavaScript layer, and the window schedules nothing while it sits idle. A release is an application bundle, not a browser engine.',
      },
      {
        q: 'What can Motty do beyond SSH?',
        a: 'SFTP and SCP with a resumable queue, ZMODEM, XMODEM, YMODEM and TRZSZ, local, remote, SOCKS5 and HTTP CONNECT forwarding with X11, jump chains, serial, Telnet and local shells, and VNC and RDP desktops in a tab. All of it shares one window.',
      },
      {
        q: 'Can an AI agent use Motty?',
        a: 'Yes. Ask and Agent modes run inside the app behind a risk-classified approval gate, and an optional MCP server bound to loopback lets external agents drive your sessions with a bearer token and an audit log. The server stays off until you enable it.',
      },
      {
        q: 'Is Motty an alternative to Termius?',
        a: 'For desktop work, yes: Motty covers SSH, SFTP, port forwarding, VNC and RDP in one native window under Apache-2.0. It does not replace Termius mobile apps or cloud vault sync, because Motty has neither — hosts and keys stay on the machine you added them to.',
      },
      {
        q: 'Does Motty use my existing ~/.ssh/config?',
        a: 'It can import it. Motty reads ~/.ssh/config to bring in hosts, keys and options, and a setting keeps the two in sync in both directions. The first time Motty rewrites the file it keeps a .motty-backup copy beside it. Imported hosts keep their names, so the same ssh command reaches them.',
      },
      {
        q: 'Can Motty chain through a jump host?',
        a: 'Yes. Jump chains are configured per host, and every hop verifies its own host key before any credential is sent, so a compromised bastion cannot silently redirect the next hop. X11 forwarding works across the chain as well. A chain is saved per host and reused like any other connection.',
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
    showcase: {
      base: {
        image: '/showcase-base-dark.webp',
        alt: 'Motty 打开 staging-web 主机时的文件面板、终端与 AI 助手',
      },
      steps: [
        {
          image: '/showcase-light.webp',
          title: '浅色、深色，一键切换',
          body: '点标题栏的主题按钮，整个窗口一起换；会话不中断。',
        },
        {
          image: '/showcase-hosts.webp',
          title: '主机按分组和标签排好',
          body: '生产、预发、CI 各自成组，标签按环境打；搜索框里敲名字直接连。',
        },
        {
          image: '/showcase-files.webp',
          title: '文件就在终端旁边',
          body: '同一会话里的 SFTP 和终端，断点续传队列和编辑器都在面板里。',
        },
        {
          image: '/showcase-colors.webp',
          title: '两百多套配色方案',
          body: 'Tokyo Night、Dracula、Nord 等精选，深色和浅色各记一套，也能导入自己的配色。',
        },
        {
          image: '/showcase-light.webp',
          title: '助手就在同一个会话里',
          body: '它直接读取这台机器的日志和进程，工具调用按风险分级审批。',
        },
      ],
    },
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
        body: '带可续传队列的 SFTP、原子替换和内置编辑器。另有 SCP 回退、ZMODEM、XMODEM/YMODEM 与 TRZSZ。',
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
    aboutTitle: 'Motty 是什么',
    about:
      'Motty 是面向 macOS、Windows 和 Linux 的免费开源 SSH 客户端，把终端、文件面板、端口转发、远程桌面和 AI 助手放进同一个原生窗口。它用 Go 写于 Viva 桌面框架之上，因此没有 Electron 运行时，也没有内嵌浏览器：窗口经 Metal、Direct3D 或 Vulkan 直接绘制，启动不到一秒，空闲时不调度任何工作。主机密钥在发送任何凭据之前先与 Motty 自己的 known_hosts 核对，保险库使用 AES-256-GCM，密钥存放在系统密钥库。同一个窗口里还有带可续传队列的 SFTP 与 SCP、本地/远程/SOCKS5/HTTP CONNECT 转发与 X11、逐跳校验的跳板链，以及可固定证书的 VNC 与 RDP 标签页。',
    testimonialsTitle: '用户怎么说',
    testimonials: pickTestimonials('zh'),
    faqTitle: '常见问题',
    faq: [
      {
        q: 'Motty 是免费的吗？',
        a: '是。Motty 以 Apache-2.0 许可开源，macOS、Windows 和 Linux 的安装包都可以免费下载。不需要账号，没有试用期，也没有付费档；应用里没有链接任何统计或遥测库。',
      },
      {
        q: 'Motty 支持哪些平台？',
        a: '支持 Apple 芯片与 Intel 的 macOS、x64 与 arm64 的 Windows，以及 x64 与 arm64 的 Linux。每个版本都提供 macOS 的 .dmg 与 .zip、Windows 的安装程序与 .zip、Linux 的 .deb 与 .tar.gz。',
      },
      {
        q: 'Motty 用 Electron 吗？',
        a: '不用。它是 Go 编写的原生应用，基于 Viva 桌面框架，直接调用 Metal、Direct3D 和 Vulkan；没有内嵌浏览器，也没有 JavaScript 层，窗口空闲时不调度任何工作。发布包是应用本体，不携带浏览器引擎。',
      },
      {
        q: '除了 SSH，Motty 还能做什么？',
        a: 'SFTP 与 SCP（带可续传队列）、ZMODEM/XMODEM/YMODEM/TRZSZ、本地/远程/SOCKS5/HTTP CONNECT 转发与 X11、跳板链、串口、Telnet 与本地 Shell，以及标签页里的 VNC 与 RDP 远程桌面。所有这些都在同一个窗口里。',
      },
      {
        q: 'AI agent 能用 Motty 吗？',
        a: '可以。应用内的 Ask 与 Agent 模式按风险分级审批；可选的 MCP 服务器只监听回环，配合 bearer token 与审计日志，外部 agent 才能驱动你的会话。该服务器默认关闭，需要你手动开启。',
      },
      {
        q: 'Motty 能替代 Termius 吗？',
        a: '桌面上的工作可以：Motty 在同一个原生窗口里提供 SSH、SFTP、端口转发、VNC 与 RDP，许可是 Apache-2.0。它替代不了 Termius 的移动端和云端保险库同步 —— Motty 没有这两项，主机与密钥只留在添加它们的那台机器上。',
      },
      {
        q: 'Motty 会用我现有的 ~/.ssh/config 吗？',
        a: '可以导入。Motty 读取 ~/.ssh/config 里的主机、密钥与选项，并有一个设置让两边双向同步；第一次改写该文件时，会在旁边保留一份 .motty-backup 副本；导入的主机保留原名，同一条 ssh 命令照样能用。',
      },
      {
        q: 'Motty 能通过跳板机连吗？',
        a: '能。跳板链按主机配置，每一跳在发送任何凭据之前都会独立校验自己的主机密钥，被攻陷的跳板机无法悄悄改写下一条连接；X11 转发也能跨整条链路工作。跳板链按主机保存，下次连接直接复用。',
      },
    ],
    footer: {
      copyright: '© 2026 Motty',
      platforms: 'macOS · Windows · Linux',
      license: 'Apache-2.0 许可',
    },
  },
};
