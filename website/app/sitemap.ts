import type { MetadataRoute } from 'next';
import { docsPath, source } from '@/lib/source';
import { absolute, hreflang, localePath, locales, type Locale } from '@/lib/seo';

export const dynamic = 'force-static';

/**
 * The home page and every doc page, in both languages, each with the other
 * languages as `xhtml:link` alternates. The root `/` is absent on purpose: it
 * answers 301 to /zh and a redirect is not a page to index.
 */
export default function sitemap(): MetadataRoute.Sitemap {
  const languages = (path: string, slugs?: string[]): Record<string, string> => {
    const map: Record<string, string> = {};
    for (const lang of locales) {
      if (!slugs || source.getPage(slugs, lang)) map[hreflang[lang]] = absolute(localePath(lang, path));
    }
    map['x-default'] = absolute(localePath('zh', path));
    return map;
  };

  const entries: MetadataRoute.Sitemap = [];

  for (const lang of locales as readonly Locale[]) {
    entries.push({
      url: absolute(localePath(lang)),
      changeFrequency: 'weekly',
      priority: lang === 'zh' ? 1 : 0.9,
      alternates: { languages: languages('') },
    });

    for (const page of source.getPages(lang)) {
      const path = docsPath(page.slugs);
      entries.push({
        url: absolute(localePath(lang, path)),
        changeFrequency: 'monthly',
        priority: page.slugs.length === 0 ? 0.8 : 0.7,
        alternates: { languages: languages(path, page.slugs) },
      });
    }
  }

  return entries;
}
