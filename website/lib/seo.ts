import type { Metadata } from 'next';
import { latestRelease } from '@/lib/releases';
import { i18n } from '@/lib/i18n';

/**
 * Everything that describes the site to a crawler lives here: the canonical
 * host, the copy that goes into titles and descriptions, and the JSON-LD the
 * pages embed. Pages only decide *which* path they are; this file decides how
 * that path is described, so a locale or host rename is a one-line change.
 *
 * The site is hosted on the apex domain; www redirects to it, and so does /.
 * Canonical URLs therefore always use the apex and the /<locale> prefix.
 */
export type Locale = 'en' | 'zh';

export const locales = i18n.languages as readonly Locale[];

export const site = {
  url: 'https://motty.page',
  name: 'Motty',
  github: 'https://github.com/t8y2/motty',
  releases: 'https://github.com/t8y2/motty/releases',
  /** The R2 mirror of the release packages, see tools/mirror-release.sh. */
  download: 'https://dl.motty.page',
} as const;

/** hreflang values. Chinese pages are Simplified Chinese, hence zh-CN. */
export const hreflang: Record<Locale, string> = { en: 'en', zh: 'zh-CN' };

export const copy: Record<Locale, { title: string; description: string; keywords: string[] }> = {
  en: {
    title: 'Motty — free native SSH client with SFTP, VNC and RDP',
    description:
      'Native desktop SSH workspace: terminal, SFTP/SCP, port forwarding, VNC and RDP desktops, and an AI assistant in one window. Go, no Electron. Free for macOS, Windows and Linux.',
    keywords: [
      'SSH client',
      'SFTP client',
      'SCP',
      'port forwarding',
      'jump host',
      'VNC client',
      'RDP client',
      'SSH terminal',
      'MCP server',
      'AI assistant',
      'macOS',
      'Windows',
      'Linux',
    ],
  },
  zh: {
    title: 'Motty — 原生 SSH 客户端：SFTP、VNC 与 RDP',
    description:
      '原生桌面 SSH 工作台：终端、SFTP/SCP、端口转发、VNC 与 RDP 远程桌面、AI 助手都在一个窗口里。Go 编写，没有 Electron，空闲时保持安静。免费开源，支持 macOS、Windows 和 Linux。',
    keywords: [
      'SSH 客户端',
      'SFTP 客户端',
      'SCP',
      '端口转发',
      '跳板机',
      'VNC 客户端',
      'RDP 客户端',
      'SSH 终端',
      'MCP 服务器',
      'AI 助手',
      'macOS',
      'Windows',
      'Linux',
    ],
  },
};

export function absolute(path: string): string {
  return new URL(path, site.url).toString();
}

/** `/zh` for the home page, `/en/docs/terminal` for a doc page. */
export function localePath(lang: Locale, path = ''): string {
  return `/${lang}${path === '/' ? '' : path}`;
}

/**
 * Canonical plus hreflang. `path` is the locale-independent part (`''` for the
 * home page, `/docs/x` for a doc page). Only pass locales whose page exists:
 * a hreflang pointing at a 404 is worse than no hreflang.
 */
export function alternates(
  lang: Locale,
  path = '',
  available: readonly Locale[] = locales,
): NonNullable<Metadata['alternates']> {
  const languages: Record<string, string> = {};
  for (const l of available) languages[hreflang[l]] = absolute(localePath(l, path));
  languages['x-default'] = absolute(localePath('zh', path));
  return { canonical: absolute(localePath(lang, path)), languages };
}


/** Image alt text: the brand once, not "Motty — Motty — ...". */
function ogAlt(title: string): string {
  return title.startsWith(site.name) ? title : `${site.name} — ${title}`;
}

export function openGraph(
  lang: Locale,
  path: string,
  title: string,
  description: string,
  type: 'website' | 'article' = 'website',
): NonNullable<Metadata['openGraph']> {
  const shared = {
    url: absolute(localePath(lang, path)),
    siteName: site.name,
    locale: lang === 'zh' ? 'zh_CN' : 'en_US',
    alternateLocale: [lang === 'zh' ? 'en_US' : 'zh_CN'],
    title,
    description,
    images: [
      {
        url: '/og.png',
        width: 1200,
        height: 630,
        alt: ogAlt(title),
      },
    ],
  };

  return type === 'article' ? { ...shared, type: 'article' } : { ...shared, type: 'website' };
}

export function twitter(title: string, description: string): NonNullable<Metadata['twitter']> {
  return {
    card: 'summary_large_image',
    title,
    description,
    images: [{ url: '/og.png', width: 1200, height: 630, alt: ogAlt(title) }],
  };
}

/** Shared crawler policy: index everything but the search API. */
export const robots: NonNullable<Metadata['robots']> = {
  index: true,
  follow: true,
  googleBot: {
    index: true,
    follow: true,
    'max-image-preview': 'large',
    'max-snippet': -1,
    'max-video-preview': -1,
  },
};

/**
 * The person behind the project, as the public GitHub account. A `Person` node
 * is what E-E-A-T means in structured data: an article `author` that resolves to
 * something, instead of the publisher pointing at itself.
 */
export const maintainer = {
  '@type': 'Person',
  '@id': `${site.url}/#maintainer`,
  name: 't8y2',
  url: 'https://github.com/t8y2',
  sameAs: ['https://github.com/t8y2'],
} as const;

export function organizationJsonLd(): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'Organization',
    '@id': `${site.url}/#organization`,
    name: site.name,
    url: site.url,
    logo: { '@type': 'ImageObject', url: absolute('/icon-512.png'), width: 512, height: 512 },
    sameAs: [site.github],
    founder: { '@id': `${site.url}/#maintainer` },
    description: copy.en.description,
  };
}

export function websiteJsonLd(lang: Locale): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'WebSite',
    '@id': `${site.url}/#website`,
    name: site.name,
    url: site.url,
    description: copy[lang].description,
    inLanguage: hreflang[lang],
    publisher: { '@id': `${site.url}/#organization` },
  };
}

export function softwareApplicationJsonLd(lang: Locale): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    '@id': `${site.url}/#software`,
    name: site.name,
    url: absolute(localePath(lang)),
    description: copy[lang].description,
    applicationCategory: 'DeveloperApplication',
    applicationSubCategory: 'SSH client',
    operatingSystem: 'macOS, Windows, Linux',
    downloadUrl: site.releases,
    installUrl: absolute(localePath(lang, '/docs/guides/install')),
    softwareHelp: absolute(localePath(lang, '/docs')),
    license: 'https://www.apache.org/licenses/LICENSE-2.0',
    isAccessibleForFree: true,
    softwareVersion: latestRelease().version,
    releaseNotes: absolute(localePath(lang, '/changelog')),
    codeRepository: site.github,
    dateModified: latestRelease().date,
    offers: { '@type': 'Offer', price: '0', priceCurrency: 'USD' },
    screenshot: absolute('/hero-dark.webp'),
    sameAs: [site.github],
    inLanguage: hreflang[lang],
  };
}

export function breadcrumbJsonLd(
  lang: Locale,
  trail: readonly { name: string; path: string }[],
): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: trail.map((crumb, index) => ({
      '@type': 'ListItem',
      position: index + 1,
      name: crumb.name,
      item: absolute(localePath(lang, crumb.path)),
    })),
  };
}

export function techArticleJsonLd(
  lang: Locale,
  path: string,
  title: string,
  description: string,
  dates: { published: string; modified: string },
  translation?: { lang: Locale; path: string },
): Record<string, unknown> {
  const url = absolute(localePath(lang, path));
  return {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    '@id': `${url}#article`,
    headline: title,
    description,
    url,
    mainEntityOfPage: url,
    image: absolute('/og.png'),
    datePublished: dates.published,
    dateModified: dates.modified,
    inLanguage: hreflang[lang],
    isPartOf: { '@id': `${site.url}/#website` },
    about: { '@id': `${site.url}/#software` },
    publisher: { '@id': `${site.url}/#organization` },
    author: { '@id': `${site.url}/#maintainer` },
    ...(translation
      ? { workTranslation: absolute(localePath(translation.lang, translation.path)) }
      : {}),
  };
}

/** FAQPage for the home page; the same questions and answers are visible there. */
export function faqJsonLd(items: { q: string; a: string }[]): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: items.map((item) => ({
      '@type': 'Question',
      name: item.q,
      acceptedAnswer: { '@type': 'Answer', text: item.a },
    })),
  };
}

/**
 * The three nodes every page needs, emitted once per document by the locale
 * layout. Article and WebPage nodes elsewhere point at these by `@id`, and a
 * reference only resolves if the node it names is in the same document — which
 * is why the graph lives in the layout rather than on the home page alone.
 */
export function siteGraph(lang: Locale): Record<string, unknown>[] {
  return [organizationJsonLd(), websiteJsonLd(lang), softwareApplicationJsonLd(lang), maintainer];
}

/**
 * A page that is not the app itself: it belongs to the site, and points at the
 * app by `@id` instead of repeating its description.
 */
export function webPageJsonLd(
  lang: Locale,
  path: string,
  title: string,
  description: string,
  extra: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    '@context': 'https://schema.org',
    '@type': 'WebPage',
    url: absolute(localePath(lang, path)),
    name: title,
    description,
    inLanguage: hreflang[lang],
    isPartOf: { '@id': `${site.url}/#website` },
    mainEntity: { '@id': `${site.url}/#software` },
    ...extra,
  };
}

/** The download page lists the packages as its significant links. */
export function downloadPageJsonLd(
  lang: Locale,
  title: string,
  description: string,
  files: string[],
): Record<string, unknown> {
  return webPageJsonLd(lang, '/download', title, description, { significantLink: files });
}
