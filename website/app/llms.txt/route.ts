import { landing } from '@/lib/landing-content';
import { docsPath, source } from '@/lib/source';
import { absolute, copy, localePath, site } from '@/lib/seo';

export const dynamic = 'force-static';

/**
 * `/llms.txt`: the site in the shape an answer engine can read in one request.
 * The format is the one llmstxt.org describes — a title, a one-line summary, and
 * links with a description each — plus the facts a citation usually needs
 * (license, platforms, downloads). The same facts appear in the HTML and in the
 * JSON-LD; this file just saves the crawler from parsing them out.
 */
export function GET() {
  const lines: string[] = [
    `# ${site.name}`,
    '',
    '> Motty is a native desktop SSH workspace: a terminal, SFTP/SCP transfer, port forwarding, VNC and RDP remote desktops, and an AI assistant in one window. It is written in Go on the Viva desktop framework, so it has no Electron runtime, no embedded browser and no JavaScript layer.',
    '',
    `- Canonical site: ${site.url} — Chinese at ${absolute('/zh')}, English at ${absolute('/en')}`,
    `- Source code (Apache-2.0): ${site.github}`,
    `- Downloads: ${site.releases}`,
    '- Platforms: macOS, Windows, Linux',
    '- Free and open source',
    '',
    '## What it does',
    '',
    '- SSH terminal: real PTY sessions, true colour, 5,000 lines of scrollback, inline images, splits, search and recording.',
    '- Hosts and authentication: host keys are verified before any credential is sent; agent, key, password and one-time codes, with an encrypted vault.',
    '- Files: SFTP with a resumable queue, atomic replacement and a built-in editor; SCP fallback; ZMODEM and XMODEM/YMODEM.',
    '- Port forwarding: local, remote, SOCKS5 and HTTP CONNECT rules, X11 forwarding, and jump chains where every hop is verified on its own.',
    '- Remote desktops: VNC and RDP in a tab, with certificate pinning, optionally through an SSH hop when the server only listens on loopback.',
    '- Assistant and MCP: Ask and Agent modes behind a risk-classified approval gate, and an optional loopback-only MCP server for external agents.',
    '',
    '## Questions and answers',
    '',
    ...landing.en.faq.flatMap((item) => [`**${item.q}**`, item.a, '']),
    '## English documentation',
    '',
    ...source
      .getPages('en')
      .map((page) => `- [${page.data.title}](${absolute(localePath('en', docsPath(page.slugs)))}): ${page.data.description}`),
    '',
    '## 中文文档',
    '',
    ...source
      .getPages('zh')
      .map((page) => `- [${page.data.title}](${absolute(localePath('zh', docsPath(page.slugs)))}): ${page.data.description}`),
    '',
    '## Machine-readable',
    '',
    `- Sitemap: ${absolute('/sitemap.xml')}`,
    `- Search index (JSON, for the site's own search box): ${absolute('/api/search')}`,
    '',
    `> ${copy.en.description}`,
    '',
  ];

  return new Response(lines.join('\n'), {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
}
