import type { Metadata } from 'next';
import Link from 'next/link';
import { JsonLd } from '@/components/json-ld';
import { download, downloadGroups, downloadHref, type Platform } from '@/lib/download-content';
import { i18n, resolveLang } from '@/lib/i18n';
import { alternates, downloadPageJsonLd, openGraph, site, twitter } from '@/lib/seo';

function PlatformIcon({ icon }: { icon: Platform }) {
  if (icon === 'macos') {
    // The same mark in black and in white, so the card follows the theme.
    return (
      <>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/icons/platform/macos.png" alt="" width={22} height={22} className="size-[22px] dark:hidden" />
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src="/icons/platform/macos-white.png" alt="" width={22} height={22} className="hidden size-[22px] dark:block" />
      </>
    );
  }

  const src = icon === 'windows' ? '/icons/platform/windows.png' : '/icons/platform/linux.svg';
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={src} alt="" width={22} height={22} className="size-[22px]" />;
}

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export async function generateMetadata({ params }: PageProps<'/[lang]/download'>): Promise<Metadata> {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  const c = download[lang];

  return {
    title: { absolute: c.title },
    description: c.description,
    alternates: alternates(lang, '/download'),
    openGraph: openGraph(lang, '/download', c.title, c.description),
    twitter: twitter(c.title, c.description),
  };
}

export default async function Page({ params }: PageProps<'/[lang]/download'>) {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  const c = download[lang];
  const groups = await downloadGroups(lang);
  const files = groups.flatMap((group) => group.files.map(downloadHref));

  return (
    <main className="mx-auto w-full max-w-5xl flex-1 px-4 pt-12 pb-16">
      <JsonLd data={downloadPageJsonLd(lang, c.title, c.description, files)} />

      <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">{c.title}</h1>

      <div className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {groups.map((group) => (
          <section key={group.platform} className="rounded-2xl border bg-fd-card p-5">
            <h2 className="flex items-center gap-3 font-medium">
              <span className="grid size-9 shrink-0 place-items-center rounded-lg border bg-fd-background">
                <PlatformIcon icon={group.icon} />
              </span>
              {group.platform}
            </h2>

            <ul className="mt-3">
              {group.files.map((file) => (
                <li key={file.key}>
                  <a
                    href={downloadHref(file)}
                    className="-mx-2 flex items-center gap-2.5 rounded-lg px-2 py-2 text-sm transition-colors hover:bg-fd-accent"
                  >
                    <span className="rounded-md border bg-fd-background px-1.5 py-1 font-mono text-[11px] leading-none text-fd-foreground/85">
                      {file.ext}
                    </span>
                    <span className="min-w-0 flex-1 truncate">{file.label}</span>
                    <span className="w-[86px] shrink-0 text-right text-[11px] text-fd-muted-foreground">
                      {file.kind}
                    </span>
                  </a>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>

      <p className="mt-6 text-sm text-fd-muted-foreground">{c.requirement}</p>

      <p className="mt-8 text-sm text-fd-muted-foreground">
        {c.sourceNote}{' '}
        <a
          href={site.releases}
          target="_blank"
          rel="noreferrer"
          className="underline-offset-4 hover:text-fd-foreground hover:underline"
        >
          {c.sourceLabel}
        </a>
        {' · '}
        <Link
          href={`/${lang}/docs/guides/install`}
          className="underline-offset-4 hover:text-fd-foreground hover:underline"
        >
          {c.install}
        </Link>
      </p>
    </main>
  );
}
