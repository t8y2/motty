import type { MetadataRoute } from 'next';
import { site } from '@/lib/seo';

export const dynamic = 'force-static';

/**
 * Answer engines fetch the page a citation points at, and several of them need
 * to be named explicitly to do it: a blanket `*` group is not enough for
 * Google-Extended or Apple's training crawler on every site. Every agent here is
 * allowed, because the docs are the thing we want quoted; only the search API is
 * excluded, since a JSON index is not a page.
 */
const answerEngineCrawlers = [
  'GPTBot',
  'OAI-SearchBot',
  'ChatGPT-User',
  'ClaudeBot',
  'Claude-SearchBot',
  'Claude-User',
  'PerplexityBot',
  'Perplexity-User',
  'Google-Extended',
  'Applebot-Extended',
  'Bingbot',
  'DuckAssistBot',
  'CCBot',
  'Amazonbot',
  'meta-externalagent',
  'YouBot',
  'cohere-ai',
];

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      { userAgent: '*', allow: '/', disallow: ['/api/'] },
      ...answerEngineCrawlers.map((userAgent) => ({
        userAgent,
        allow: '/',
        disallow: ['/api/'],
      })),
    ],
    sitemap: `${site.url}/sitemap.xml`,
  };
}
