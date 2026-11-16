/**
 * The comparison page: one answer-shaped page for the queries a category reader
 * actually types ("best free SSH client for macOS", "Termius alternative").
 *
 * Every competitor cell is something their public site or repository said on the
 * date below — no benchmarks, no invented pricing, no "better" without a reason.
 * The page is written to be quoted: an answer-first introduction, a real HTML
 * table, and an explicit section for where Motty is the wrong choice.
 */
export type CompareCell = string;

export type CompareContent = {
  title: string;
  description: string;
  h1: string;
  intro: string;
  tableCaption: string;
  checked: string;
  columns: { motty: string; termius: string; tabby: string; windterm: string };
  rows: { label: string; motty: CompareCell; termius: CompareCell; tabby: CompareCell; windterm: CompareCell }[];
  winsTitle: string;
  wins: string[];
  lossesTitle: string;
  losses: string[];
  chooseTitle: string;
  choose: string[];
  faqTitle: string;
  faq: { q: string; a: string }[];
  footnote: string;
};

export const compare: Record<'en' | 'zh', CompareContent> = {
  en: {
    title: 'Motty compared with Termius, Tabby and WindTerm',
    description:
      'A feature and licence comparison of free SSH clients for macOS, Windows and Linux: Motty, Termius, Tabby and WindTerm — native versus Electron, VNC and RDP, and MCP support.',
    h1: 'Motty compared with Termius, Tabby and WindTerm',
    intro:
      'Motty is a free, open-source (Apache-2.0) SSH client for macOS, Windows and Linux, written in Go on the Viva desktop framework, so it runs no Electron runtime and no embedded browser. A terminal, SFTP and SCP transfer, port forwarding, VNC and RDP desktops, and an MCP server for external agents share one window. Termius is a commercial client with mobile apps and a free Starter tier; Tabby is an MIT-licensed terminal built around a plugin ecosystem; WindTerm is a free terminal whose released source is partially open. The table below compares what each project documents about itself, not what a benchmark says.',
    tableCaption: 'Features as documented by each project, checked 2026-10-29',
    checked:
      'Checked 2026-10-29 against termius.com/pricing, tabby.sh and the Tabby repository, and the WindTerm repository. These products ship often — confirm anything you depend on with the vendor.',
    columns: { motty: 'Motty', termius: 'Termius', tabby: 'Tabby', windterm: 'WindTerm' },
    rows: [
      {
        label: 'Licence',
        motty: 'Apache-2.0',
        termius: 'Proprietary',
        tabby: 'MIT',
        windterm: 'Partially open source (Apache-2.0 for the released code)',
      },
      {
        label: 'Price for one person',
        motty: 'Free',
        termius: 'Free Starter tier; Pro $10/month billed annually',
        tabby: 'Free',
        windterm: 'Free',
      },
      {
        label: 'Platforms',
        motty: 'macOS, Windows, Linux',
        termius: 'macOS, Windows, Linux, iOS, Android',
        tabby: 'macOS, Windows, Linux',
        windterm: 'macOS, Windows, Linux',
      },
      {
        label: 'SSH and file transfer',
        motty: 'SSH terminal, SFTP and SCP with a resumable queue, ZMODEM and XMODEM/YMODEM',
        termius: 'SSH terminal and SFTP',
        tabby: 'SSH terminal and SFTP',
        windterm: 'SSH, Telnet, serial and SFTP',
      },
      {
        label: 'Port forwarding',
        motty: 'Local, remote, SOCKS5 and HTTP CONNECT, plus X11 and jump chains',
        termius: 'Yes',
        tabby: 'Yes',
        windterm: 'Not documented on the project page',
      },
      {
        label: 'VNC and RDP in the same window',
        motty: 'Yes, with certificate pinning and optional SSH tunnelling',
        termius: 'No',
        tabby: 'No',
        windterm: 'No',
      },
      {
        label: 'MCP server for external agents',
        motty: 'Yes: loopback only, bearer token, audit log',
        termius: 'No',
        tabby: 'No',
        windterm: 'No',
      },
      {
        label: 'Built-in assistant',
        motty: 'Ask and Agent modes behind a risk-classified approval gate',
        termius: 'AI autocomplete on every tier',
        tabby: 'No',
        windterm: 'No',
      },
    ],
    winsTitle: 'Where Motty is ahead',
    wins: [
      'One window for the whole session: a terminal, a file panel, forwarding rules, VNC and RDP tabs, so a server that needs three tools in three apps needs one here.',
      'No Electron runtime: the window is drawn through Metal, Direct3D or Vulkan, which is why start-up is short and an idle window schedules no work.',
      'An MCP server that is off by default, bound to loopback and protected by a bearer token, so an external agent can drive a session without a network listener.',
      'Apache-2.0 source, including the SSH, forwarding and terminal layers — not just the plugin API.',
    ],
    lossesTitle: 'Where Motty is behind',
    losses: [
      'No mobile app and no cloud vault: Termius syncs hosts and keys across desktop and phone, and Motty does not.',
      'Six days old when this page was written: fewer years of edge cases, no long third-party review trail, and no team features.',
      'One maintainer rather than a company: the issue tracker is the support channel, with no SLA and no commercial contract.',
    ],
    chooseTitle: 'Which one to pick',
    choose: [
      'Choose Motty if you want a native, open-source SSH client on the desktop and you would rather have SFTP, forwarding and remote desktops in the same window than in four.',
      'Choose Termius if you need the same hosts on your phone, or a commercial support path.',
      'Choose Tabby if you want an MIT-licensed terminal with a deep plugin ecosystem and you already live in its configuration.',
      'Choose WindTerm if you need serial, Telnet and SFTP in one free terminal and do not need VNC, RDP or agent access.',
    ],
    faqTitle: 'Questions this comparison raises',
    faq: [
      {
        q: 'Is Motty a drop-in replacement for Termius?',
        a: 'For desktop SSH, SFTP and port forwarding work, yes: Motty opens the same hosts and gives the terminal and file panel one window. It is not a drop-in replacement for Termius mobile apps or cloud vault sync, because Motty has neither — hosts and keys stay on the machine where you added them.',
      },
      {
        q: 'Is Motty really free?',
        a: 'Yes. Motty is licensed under Apache-2.0 and every package for macOS, Windows and Linux downloads at no cost, with no account, no trial and no paid tier. The source, the issue tracker and the releases are all public on GitHub, and no analytics or telemetry library is linked into the application.',
      },
      {
        q: 'Can Motty and Termius be installed side by side?',
        a: 'Yes. Both are ordinary desktop applications, and Motty keeps its own host list, vault and settings, so installing it does not touch an existing SSH client. Motty can import OpenSSH config and keys, which makes a side-by-side trial cheap and reversible.',
      },
    ],
    footnote:
      'Termius, Tabby and WindTerm are trademarks of their respective owners. This page is maintained by the Motty project and states where the comparison is unflattering; corrections with a source are welcome in the issue tracker.',
  },
  zh: {
    title: 'Motty 与 Termius、Tabby、WindTerm 对比',
    description:
      'macOS、Windows、Linux 上免费 SSH 客户端的功能与许可对比：Motty、Termius、Tabby、WindTerm —— 原生还是 Electron、是否内置 VNC 与 RDP、是否支持 MCP。',
    h1: 'Motty 与 Termius、Tabby、WindTerm 对比',
    intro:
      'Motty 是面向 macOS、Windows 和 Linux 的免费开源（Apache-2.0）SSH 客户端，用 Go 写于 Viva 桌面框架之上，因此不运行 Electron 运行时，也没有内嵌浏览器。终端、SFTP 与 SCP 传输、端口转发、VNC 与 RDP 远程桌面，以及供外部 agent 使用的 MCP 服务器，都在同一个窗口里。Termius 是带移动端和免费 Starter 档的商业客户端；Tabby 是以插件生态为核心的 MIT 许可终端；WindTerm 是免费的终端，已公开的源码采用部分开源。下表对比的是各自公开文档里的说法，不是跑分结论。',
    tableCaption: '各项目自行公开的功能，核对于 2026-10-29',
    checked:
      '核对于 2026-10-29，来源为 termius.com/pricing、tabby.sh 与 Tabby 仓库、WindTerm 仓库。这些产品迭代很快 —— 依赖具体能力前请向厂商确认。',
    columns: { motty: 'Motty', termius: 'Termius', tabby: 'Tabby', windterm: 'WindTerm' },
    rows: [
      {
        label: '许可',
        motty: 'Apache-2.0',
        termius: '专有',
        tabby: 'MIT',
        windterm: '部分开源（已公开的源码为 Apache-2.0）',
      },
      {
        label: '个人价格',
        motty: '免费',
        termius: '免费 Starter 档；Pro 按年付费 10 美元/月',
        tabby: '免费',
        windterm: '免费',
      },
      {
        label: '平台',
        motty: 'macOS、Windows、Linux',
        termius: 'macOS、Windows、Linux、iOS、Android',
        tabby: 'macOS、Windows、Linux',
        windterm: 'macOS、Windows、Linux',
      },
      {
        label: 'SSH 与文件传输',
        motty: 'SSH 终端、带可续传队列的 SFTP 与 SCP、ZMODEM 与 XMODEM/YMODEM',
        termius: 'SSH 终端与 SFTP',
        tabby: 'SSH 终端与 SFTP',
        windterm: 'SSH、Telnet、串口与 SFTP',
      },
      {
        label: '端口转发',
        motty: '本地、远程、SOCKS5、HTTP CONNECT，另有 X11 与跳板链',
        termius: '支持',
        tabby: '支持',
        windterm: '项目页面未说明',
      },
      {
        label: '同一窗口内的 VNC 与 RDP',
        motty: '支持，带证书固定与可选 SSH 中转',
        termius: '不支持',
        tabby: '不支持',
        windterm: '不支持',
      },
      {
        label: '供外部 agent 使用的 MCP 服务器',
        motty: '支持：仅回环、bearer token、审计日志',
        termius: '不支持',
        tabby: '不支持',
        windterm: '不支持',
      },
      {
        label: '内置助手',
        motty: 'Ask 与 Agent 模式，按风险分级审批',
        termius: '各档均含 AI 补全',
        tabby: '不支持',
        windterm: '不支持',
      },
    ],
    winsTitle: 'Motty 领先的地方',
    wins: [
      '一个窗口装下一次会话：终端、文件面板、转发规则、VNC 与 RDP 标签页，过去要开三个应用的服务器这里只需一个。',
      '没有 Electron 运行时：窗口经 Metal、Direct3D 或 Vulkan 直接绘制，所以启动快，空闲窗口不调度任何工作。',
      'MCP 服务器默认关闭、只监听回环、带 bearer token，外部 agent 可以驱动会话而不必暴露网络端口。',
      'Apache-2.0 覆盖 SSH、转发与终端这些核心层，不只是插件接口。',
    ],
    lossesTitle: 'Motty 落后的地方',
    losses: [
      '没有移动端，也没有云端保险库：Termius 能在桌面和手机之间同步主机与密钥，Motty 不能。',
      '撰写本页时项目只有六天大：边界情况打磨的年头少，没有长期的第三方评测记录，也没有团队功能。',
      '一位维护者而不是一家公司：支持渠道就是 issue，没有 SLA，也没有商业合同。',
    ],
    chooseTitle: '怎么选',
    choose: [
      '想要桌面上的原生开源 SSH 客户端，并且希望 SFTP、转发和远程桌面在同一窗口而不是四个窗口里，选 Motty。',
      '需要在手机上用同一批主机，或者需要商业支持渠道，选 Termius。',
      '想要 MIT 许可、插件生态很深的终端，并且已经习惯它的配置，选 Tabby。',
      '需要在一个免费终端里同时有串口、Telnet 和 SFTP，且不需要 VNC、RDP 或 agent 接入，选 WindTerm。',
    ],
    faqTitle: '关于这个对比的常见问题',
    faq: [
      {
        q: 'Motty 能直接替代 Termius 吗？',
        a: '桌面上的 SSH、SFTP 与端口转发可以：Motty 打开同一批主机，把终端和文件面板放进一个窗口。但 Termius 的移动端和云端保险库同步它替代不了 —— Motty 没有这两项，主机与密钥只留在添加它们的那台机器上。',
      },
      {
        q: 'Motty 真的免费吗？',
        a: '是。Motty 以 Apache-2.0 许可发布，macOS、Windows 和 Linux 的安装包全部免费下载，不需要账号、没有试用期、也没有付费档。源码、issue 和发布包都在 GitHub 上公开，应用里没有链接任何统计或遥测库。',
      },
      {
        q: 'Motty 和 Termius 可以同时安装吗？',
        a: '可以。两者都是普通桌面应用，Motty 维护自己的主机列表、保险库和设置，安装它不会改动已有的 SSH 客户端。Motty 能导入 OpenSSH 配置和密钥，因此并行试用成本很低，也随时可以撤回。',
      },
    ],
    footnote:
      'Termius、Tabby、WindTerm 是其各自所有者的商标。本页由 Motty 项目维护，并且写明了对比中对 Motty 不利的部分；附上来源的更正欢迎提交到 issue。',
  },
};
