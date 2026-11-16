import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { zhCN } from '@fumadocs/language/zh-cn';
import { uiTranslations } from 'fumadocs-ui/i18n';
import { i18n, resolveLang } from '@/lib/i18n';

export const translations = i18n
  .translations()
  .extend(uiTranslations())
  .add({ en: { displayName: 'English' } })
  .preset('zh', zhCN())
  .add({ zh: { displayName: '简体中文' } });

function Logo() {
  return (
    <>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/logo-light.svg" alt="Motty" className="h-6 w-auto dark:hidden" />
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/logo-dark.svg" alt="Motty" className="hidden h-6 w-auto dark:block" />
    </>
  );
}

export function baseOptions(locale: string): BaseLayoutProps {
  const lang = resolveLang(locale);
  return {
    nav: {
      title: <Logo />,
      url: `/${lang}`,
    },
    githubUrl: 'https://github.com/t8y2/motty',
    links: [
      {
        type: 'main',
        text: lang === 'zh' ? '文档' : 'Documentation',
        url: `/${lang}/docs`,
      },
      {
        type: 'main',
        text: lang === 'zh' ? '下载' : 'Download',
        url: `/${lang}/download`,
      },
      {
        type: 'main',
        text: lang === 'zh' ? '更新日志' : 'Changelog',
        url: `/${lang}/changelog`,
      },
      {
        type: 'main',
        text: lang === 'zh' ? '对比' : 'Compare',
        url: `/${lang}/compare`,
      },
    ],
  };
}
