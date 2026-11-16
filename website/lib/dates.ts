import { execFileSync } from 'node:child_process';

/**
 * When a page last changed, for the dates search and answer engines read
 * (`sitemap.xml` `<lastmod>`, JSON-LD `datePublished`/`dateModified`) and for the
 * "last updated" line the docs print under a page.
 *
 * The source of truth is git: a static export has no CMS timestamps, and asking
 * the author to bump a frontmatter field on every edit is a promise nobody
 * keeps. The build runs inside the repository, so the commit date of the file
 * that produced the page is the honest answer.
 *
 * CI checks out with full history (`.github/workflows/website.yml` sets
 * `fetch-depth: 0`); a shallow clone or a source tarball has no history for an
 * untouched file, and those builds fall back to the date below rather than
 * claiming the build time, which would read as "every page changed today".
 */
const FALLBACK = '2026-10-29';

/** The last commit that touched `file`, as `YYYY-MM-DD`, or the fallback. */
export function lastCommitDate(file: string): string {
  try {
    const out = execFileSync('git', ['log', '-1', '--format=%cs', '--', file], {
      cwd: process.cwd(),
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
    if (/^\d{4}-\d{2}-\d{2}$/.test(out)) return out;
  } catch {
    // No git, no history, or an unreadable repository: use the fallback.
  }
  return FALLBACK;
}

/** The source file behind a docs page: `content/docs/terminal/terminal.mdx`. */
export function docsSourceFile(slugs: readonly string[], lang: 'en' | 'zh'): string {
  const base = slugs.length > 0 ? `content/docs/${slugs.join('/')}` : 'content/docs/index';
  return lang === 'zh' ? `${base}.zh.mdx` : `${base}.mdx`;
}

/** `datePublished` and `dateModified` for a docs page (both from the same file). */
export function docsDates(
  slugs: readonly string[],
  lang: 'en' | 'zh',
): { published: string; modified: string } {
  const date = lastCommitDate(docsSourceFile(slugs, lang));
  return { published: date, modified: date };
}

/** A human "last updated" label in the page's locale. */
export function formatUpdated(date: string, lang: 'en' | 'zh'): string {
  const [year, month, day] = date.split('-').map(Number);
  const formatted = new Intl.DateTimeFormat(lang === 'zh' ? 'zh-CN' : 'en-US', {
    dateStyle: 'long',
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(year, month - 1, day)));
  return lang === 'zh' ? `最后更新：${formatted}` : `Last updated: ${formatted}`;
}
