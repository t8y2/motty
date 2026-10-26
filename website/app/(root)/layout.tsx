import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { site } from '@/lib/seo';

/**
 * `/` answers 301 to the Chinese home page on Cloudflare Pages (public/_redirects).
 * This HTML only survives as a fallback for `next dev` or another static host, so
 * it must never be indexed: a crawler that reaches it should follow the refresh
 * and index /zh instead.
 */
export const metadata: Metadata = {
  metadataBase: new URL(site.url),
  title: 'Motty',
  robots: { index: false, follow: true },
  icons: { icon: '/icon.svg', apple: '/apple-touch-icon.png' },
};

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
