import { defineI18n } from 'fumadocs-core/i18n';

/**
 * `defaultLanguage` follows the file convention, not the site's entry point:
 * fumadocs reads `page.mdx` as the default language and `page.<lang>.mdx` as a
 * translation, so the unsuffixed files (which are English) are `en`. Setting it
 * to `zh` made every /en/docs page fall back to the Chinese body — English pages
 * that rendered Chinese — so keep it aligned with the files.
 *
 * Chinese is still the first language a visitor sees: `/` answers 301 to /zh in
 * public/_redirects, and the metadata marks x-default as /zh.
 */
export const i18n = defineI18n({
  defaultLanguage: 'en',
  languages: ['en', 'zh'],
});

export type Lang = (typeof i18n.languages)[number];

export function resolveLang(lang: string): Lang {
  return (i18n.languages as readonly string[]).includes(lang) ? (lang as Lang) : i18n.defaultLanguage;
}
