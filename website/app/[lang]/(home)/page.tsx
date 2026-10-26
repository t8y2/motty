import type { Metadata } from 'next';
import { JsonLd } from '@/components/json-ld';
import { Landing } from '@/components/landing';
import { landing } from '@/lib/landing-content';
import { i18n, resolveLang } from '@/lib/i18n';
import {
  alternates,
  copy,
  faqJsonLd,
  openGraph,
  organizationJsonLd,
  softwareApplicationJsonLd,
  twitter,
  websiteJsonLd,
} from '@/lib/seo';

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export async function generateMetadata({ params }: PageProps<'/[lang]'>): Promise<Metadata> {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);
  const text = copy[lang];

  return {
    title: { absolute: text.title },
    description: text.description,
    alternates: alternates(lang),
    openGraph: openGraph(lang, '', text.title, text.description),
    twitter: twitter(text.title, text.description),
  };
}

export default async function Page({ params }: PageProps<'/[lang]'>) {
  const { lang: raw } = await params;
  const lang = resolveLang(raw);

  return (
    <>
      <JsonLd
        data={[
          organizationJsonLd(),
          websiteJsonLd(lang),
          softwareApplicationJsonLd(lang),
          faqJsonLd(landing[lang].faq),
        ]}
      />
      <Landing lang={lang} />
    </>
  );
}
