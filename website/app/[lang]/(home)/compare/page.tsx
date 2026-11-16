import type { Metadata } from 'next';
import { JsonLd } from '@/components/json-ld';
import { compare } from '@/lib/compare-content';
import { lastCommitDate } from '@/lib/dates';
import { i18n, resolveLang } from '@/lib/i18n';
import { alternates, copy, faqJsonLd, openGraph, site, twitter, webPageJsonLd } from '@/lib/seo';

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export async function generateMetadata({ params }: PageProps<'/[lang]/compare'>): Promise<Metadata> {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  const text = compare[lang];
  const socialTitle = `${text.title} | ${site.name}`;

  return {
    title: text.h1,
    description: text.description,
    alternates: alternates(lang, '/compare'),
    openGraph: openGraph(lang, '/compare', socialTitle, text.description, 'article'),
    twitter: twitter(socialTitle, text.description),
  };
}

export default async function Page({ params }: PageProps<'/[lang]/compare'>) {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  const text = compare[lang];
  const other = lang === 'zh' ? 'en' : 'zh';

  return (
    <>
      <JsonLd
        data={[
          webPageJsonLd(lang, '/compare', text.title, text.description, {
            dateModified: lastCommitDate('lib/compare-content.ts'),
          }),
          faqJsonLd(text.faq),
        ]}
      />
      <article className="mx-auto w-full max-w-5xl px-4 py-16">
        <h1 className="text-3xl font-semibold tracking-tight text-balance sm:text-4xl">{text.h1}</h1>
        <p className="mt-6 text-lg leading-relaxed text-fd-muted-foreground">{text.intro}</p>

        <div className="mt-10 w-full max-w-full overflow-x-auto rounded-2xl border">
          <table className="w-full min-w-[680px] border-collapse text-left text-sm">
            <caption className="border-b bg-fd-secondary/40 px-4 py-3 text-left text-xs text-fd-muted-foreground">
              {text.tableCaption}
            </caption>
            <thead>
              <tr className="bg-fd-card">
                <th scope="col" className="px-4 py-3 font-medium">
                  {lang === 'zh' ? '能力' : 'Capability'}
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  {text.columns.motty}
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  {text.columns.termius}
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  {text.columns.tabby}
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  {text.columns.windterm}
                </th>
              </tr>
            </thead>
            <tbody>
              {text.rows.map((row) => (
                <tr key={row.label} className="border-t align-top">
                  <th scope="row" className="px-4 py-3 font-medium">
                    {row.label}
                  </th>
                  <td className="px-4 py-3">{row.motty}</td>
                  <td className="px-4 py-3 text-fd-muted-foreground">{row.termius}</td>
                  <td className="px-4 py-3 text-fd-muted-foreground">{row.tabby}</td>
                  <td className="px-4 py-3 text-fd-muted-foreground">{row.windterm}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="mt-3 text-xs text-fd-muted-foreground">{text.checked}</p>

        <section className="mt-14">
          <h2 className="text-2xl font-semibold tracking-tight">{text.winsTitle}</h2>
          <ul className="mt-4 space-y-3 leading-relaxed text-fd-muted-foreground">
            {text.wins.map((item) => (
              <li key={item} className="flex gap-3">
                <span aria-hidden="true" className="text-fd-primary">
                  +
                </span>
                <span>{item}</span>
              </li>
            ))}
          </ul>
        </section>

        <section className="mt-10">
          <h2 className="text-2xl font-semibold tracking-tight">{text.lossesTitle}</h2>
          <ul className="mt-4 space-y-3 leading-relaxed text-fd-muted-foreground">
            {text.losses.map((item) => (
              <li key={item} className="flex gap-3">
                <span aria-hidden="true" className="text-fd-muted-foreground">
                  −
                </span>
                <span>{item}</span>
              </li>
            ))}
          </ul>
        </section>

        <section className="mt-10">
          <h2 className="text-2xl font-semibold tracking-tight">{text.chooseTitle}</h2>
          <ul className="mt-4 space-y-3 leading-relaxed text-fd-muted-foreground">
            {text.choose.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </section>

        <section className="mt-14">
          <h2 className="text-2xl font-semibold tracking-tight">{text.faqTitle}</h2>
          <dl className="mt-6 space-y-6">
            {text.faq.map((item) => (
              <div key={item.q}>
                <dt className="font-medium">{item.q}</dt>
                <dd className="mt-2 leading-relaxed text-fd-muted-foreground">{item.a}</dd>
              </div>
            ))}
          </dl>
        </section>

        <p className="mt-14 border-t pt-6 text-xs leading-relaxed text-fd-muted-foreground">{text.footnote}</p>
        <p className="mt-2 text-xs text-fd-muted-foreground">
          <a className="underline" href={`/${other}/compare`} hrefLang={other === 'zh' ? 'zh-CN' : 'en'}>
            {other === 'zh' ? '中文对比' : 'Read this comparison in English'}
          </a>
        </p>
      </article>
    </>
  );
}
