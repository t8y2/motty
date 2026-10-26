import { DocsBody, DocsDescription, DocsPage, DocsTitle } from 'fumadocs-ui/layouts/docs/page';
import { notFound } from 'next/navigation';
import type { Metadata } from 'next';
import { JsonLd } from '@/components/json-ld';
import { getMDXComponents } from '@/components/mdx';
import { resolveLang } from '@/lib/i18n';
import {
  alternates,
  breadcrumbJsonLd,
  copy,
  locales,
  openGraph,
  site,
  techArticleJsonLd,
  twitter,
} from '@/lib/seo';
import { docsPath, source } from '@/lib/source';

export default async function Page(props: PageProps<'/[lang]/docs/[[...slug]]'>) {
  const params = await props.params;
  const page = source.getPage(params.slug, params.lang);
  if (!page) notFound();

  const lang = resolveLang(params.lang);
  const path = docsPath(page.slugs);
  const description = page.data.description ?? copy[lang].description;
  const MDX = page.data.body;

  const trail = [{ name: 'Motty', path: '' }, { name: lang === 'zh' ? '文档' : 'Documentation', path: '/docs' }];
  if (page.slugs.length > 0) trail.push({ name: page.data.title, path });

  return (
    <>
      <JsonLd
        data={[
          breadcrumbJsonLd(lang, trail),
          techArticleJsonLd(lang, path, page.data.title, description),
        ]}
      />
      <DocsPage toc={page.data.toc}>
        <DocsTitle>{page.data.title}</DocsTitle>
        <DocsDescription>{page.data.description}</DocsDescription>
        <DocsBody>
          <MDX components={getMDXComponents()} />
        </DocsBody>
      </DocsPage>
    </>
  );
}

export function generateStaticParams() {
  return source.generateParams();
}

export async function generateMetadata(props: PageProps<'/[lang]/docs/[[...slug]]'>): Promise<Metadata> {
  const params = await props.params;
  const page = source.getPage(params.slug, params.lang);
  if (!page) notFound();

  const lang = resolveLang(params.lang);
  const text = copy[lang];
  const isIndex = page.slugs.length === 0;
  // The docs index is titled "Motty" in its frontmatter; as a page title that
  // would read "Motty | Motty", so it gets an absolute title instead.
  const title = isIndex ? `${site.name} — ${lang === 'zh' ? '文档' : 'Documentation'}` : page.data.title;
  const description = page.data.description ?? text.description;
  const path = docsPath(page.slugs);
  // Only advertise a translation that exists: the docs are written in pairs,
  // but a one-language page must not point hreflang at a 404.
  const available = locales.filter((candidate) => source.getPage(page.slugs, candidate) !== undefined);

  // The <title> template does not reach the card tags, so build the same shape
  // here: "Install | Motty" reads better on a share card than "Install".
  const socialTitle = isIndex ? title : `${title} | ${site.name}`;

  return {
    title: isIndex ? { absolute: title } : title,
    description,
    alternates: alternates(lang, path, available),
    openGraph: openGraph(lang, path, socialTitle, description, 'article'),
    twitter: twitter(socialTitle, description),
  };
}
