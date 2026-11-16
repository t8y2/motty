import { loader } from 'fumadocs-core/source';
import { defineDocs } from 'fumadocs-mdx/macro';
import { i18n } from '@/lib/i18n';

const docs = defineDocs({
  dir: 'content/docs',
});

export const source = loader({
  baseUrl: '/docs',
  source: docs.toFumadocsSource(),
  i18n,
});

/** The locale-independent path of a docs page: `/docs/terminal`. */
export function docsPath(slugs: readonly string[] | undefined): string {
  return slugs && slugs.length > 0 ? `/docs/${slugs.join('/')}` : '/docs';
}
