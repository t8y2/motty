/**
 * The changelog's copy and anchors. The releases themselves come from
 * `lib/releases`, which reads `docs/releases/*.md`: the page has no second copy
 * of the notes to keep in step.
 */
export const changelogPage: Record<
  'en' | 'zh',
  {
    title: string;
    description: string;
    versions: string;
    publishedOn: string;
    download: string;
    tocSubtitle: string;
    /** `In v0.1.0` above the section list of the release in view. */
    tocTitle: (version: string) => string;
  }
> = {
  en: {
    title: 'Changelog',
    description: 'Track every release: features, improvements and fixes.',
    versions: 'Versions',
    publishedOn: 'Published on',
    download: 'Download',
    tocSubtitle: 'Release contents',
    tocTitle: (version) => `In ${version}`,
  },
  zh: {
    title: '更新日志',
    description: '追踪每次发布：新功能、改进和修复。',
    versions: '版本列表',
    publishedOn: '发布于',
    download: '下载',
    tocSubtitle: '更新内容',
    tocTitle: (version) => `在 ${version} 中`,
  },
};

/** `v0.1.0`, the way the tag and the release title spell it. */
export function versionLabel(version: string): string {
  return `v${version}`;
}

/** Anchor ids in the same shape the DBX changelog uses. */
export function releaseId(version: string): string {
  return `changelog-release-v${version.replace(/\./g, '-')}`;
}

export function sectionId(version: string, index: number): string {
  return `${releaseId(version)}-section-${index}`;
}
