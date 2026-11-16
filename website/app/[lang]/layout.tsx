import '../global.css';
import type { Metadata, Viewport } from 'next';
import { Inter } from 'next/font/google';
import { JsonLd } from '@/components/json-ld';
import { Provider } from '@/components/provider';
import { i18n, resolveLang } from '@/lib/i18n';
import { copy, robots, site, siteGraph } from '@/lib/seo';

const inter = Inter({ subsets: ['latin'] });

export async function generateMetadata({ params }: LayoutProps<'/[lang]'>): Promise<Metadata> {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  const text = copy[lang];

  return {
    metadataBase: new URL(site.url),
    title: { default: text.title, template: `%s | ${site.name}` },
    description: text.description,
    keywords: text.keywords,
    applicationName: site.name,
    authors: [{ name: site.name, url: site.github }],
    creator: site.name,
    publisher: site.name,
    category: 'DeveloperApplication',
    robots,
    icons: { icon: '/icon.svg', apple: '/apple-touch-icon.png' },
    // Canonical and the hreflang set belong to each page: a doc page under /en
    // must not advertise the home page as its canonical.
  };
}

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#ffffff' },
    { media: '(prefers-color-scheme: dark)', color: '#0a0a0a' },
  ],
};

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export const dynamicParams = false;

export default async function Layout({ params, children }: LayoutProps<'/[lang]'>) {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  return (
    <html lang={lang === 'zh' ? 'zh-CN' : lang} className={inter.className} suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        {/* Organization, WebSite, SoftwareApplication and the maintainer, once
            per document: every other node on the page references these by @id. */}
        <JsonLd data={siteGraph(lang)} />
        <Provider lang={lang}>{children}</Provider>
      </body>
    </html>
  );
}
