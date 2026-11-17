import { site } from '@/lib/seo';

/**
 * The /download page. The packages live on the R2 mirror (tools/mirror-release.sh
 * publishes `releases/latest/<version-less name>` for every release), so a link
 * never needs a site rebuild per version.
 *
 * `catalog` lists the packages a release may contain and `latest.json` says which
 * ones this release actually has, so a format that is added or dropped with a
 * release disappears from the page on the next build instead of leaving a dead
 * link. When the manifest cannot be read the whole catalog is shown.
 */
export type Platform = 'macos' | 'windows' | 'linux';

export type FileKind = 'dmg' | 'zip' | 'exe' | 'deb' | 'targz';

export type DownloadFile = {
  /** `releases/latest/<key>` under the mirror host. */
  key: string;
  label: string;
  /** The file extension, shown as a badge before the label. */
  ext: string;
  /** The architecture or CPU family, shown at the end of the row. */
  kind: string;
};

export type DownloadGroup = {
  platform: string;
  icon: Platform;
  files: DownloadFile[];
};

export type DownloadContent = {
  title: string;
  description: string;
  platformLabels: Record<Platform, string>;
  requirement: string;
  sourceNote: string;
  sourceLabel: string;
  install: string;
};

const kinds: Record<FileKind, { ext: string; en: string; zh: string }> = {
  dmg: { ext: '.dmg', en: 'Disk image', zh: '磁盘映像' },
  zip: { ext: '.zip', en: 'Zip', zh: 'Zip' },
  exe: { ext: '.exe', en: 'Installer', zh: '安装程序' },
  deb: { ext: '.deb', en: 'Debian package', zh: 'Debian 包' },
  targz: { ext: '.tar.gz', en: 'Tarball', zh: '压缩包' },
};

const portable = { en: 'Portable zip', zh: '便携版 Zip' };

/** One line per package a release can contain, in the order the page shows it. */
const catalog: {
  key: string;
  platform: Platform;
  kind: FileKind;
  arch: 'arm64' | 'amd64';
  /** Replaces the kind's name when the file is more specific than its format. */
  label?: { en: string; zh: string };
}[] = [
  { key: 'motty-darwin-arm64.dmg', platform: 'macos', kind: 'dmg', arch: 'arm64' },
  { key: 'motty-darwin-amd64.dmg', platform: 'macos', kind: 'dmg', arch: 'amd64' },
  { key: 'motty-darwin-arm64.zip', platform: 'macos', kind: 'zip', arch: 'arm64' },
  { key: 'motty-darwin-amd64.zip', platform: 'macos', kind: 'zip', arch: 'amd64' },
  { key: 'motty-windows-amd64-setup.exe', platform: 'windows', kind: 'exe', arch: 'amd64' },
  { key: 'motty-windows-arm64-setup.exe', platform: 'windows', kind: 'exe', arch: 'arm64' },
  { key: 'motty-windows-amd64.zip', platform: 'windows', kind: 'zip', arch: 'amd64', label: portable },
  { key: 'motty-windows-arm64.zip', platform: 'windows', kind: 'zip', arch: 'arm64', label: portable },
  { key: 'motty_amd64.deb', platform: 'linux', kind: 'deb', arch: 'amd64' },
  { key: 'motty_arm64.deb', platform: 'linux', kind: 'deb', arch: 'arm64' },
  { key: 'motty-linux-amd64.tar.gz', platform: 'linux', kind: 'targz', arch: 'amd64' },
  { key: 'motty-linux-arm64.tar.gz', platform: 'linux', kind: 'targz', arch: 'arm64' },
];

const archLabel = (platform: Platform, arch: 'arm64' | 'amd64'): string => {
  if (platform === 'macos') return arch === 'arm64' ? 'Apple silicon' : 'Intel';
  return arch === 'arm64' ? 'arm64' : 'x64';
};

export const download: Record<'en' | 'zh', DownloadContent> = {
  en: {
    title: 'Download Motty',
    description:
      'Packages for macOS, Windows and Linux, served from Cloudflare R2. GitHub Releases stays the source of record.',
    platformLabels: { macos: 'macOS', windows: 'Windows', linux: 'Linux' },
    requirement: 'Windows packages need Windows 10 or Windows Server 2016 or later.',
    sourceNote: 'The release, its notes and its checksums live on',
    sourceLabel: 'GitHub Releases',
    install: 'Installation notes',
  },
  zh: {
    title: '下载 Motty',
    description: 'macOS、Windows 和 Linux 安装包，由 Cloudflare R2 提供下载。GitHub Releases 仍是源头。',
    platformLabels: { macos: 'macOS', windows: 'Windows', linux: 'Linux' },
    requirement: 'Windows 安装包需要 Windows 10 或 Windows Server 2016 及更高版本。',
    sourceNote: '版本本身、更新说明和校验文件都在',
    sourceLabel: 'GitHub Releases',
    install: '安装说明',
  },
};

/** The version-less names the mirror currently serves, or null when it is unreachable. */
async function liveFiles(): Promise<Set<string> | null> {
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const response = await fetch(`${site.download}/latest.json`, {
        cache: 'force-cache',
        headers: { Accept: 'application/json' },
        signal: AbortSignal.timeout(5000),
      });
      if (!response.ok) continue;
      const manifest: { files?: { name?: string }[] } = await response.json();
      const names = (manifest.files ?? [])
        .map((file) => file.name)
        .filter((name): name is string => Boolean(name));
      if (names.length > 0) return new Set(names);
    } catch {
      // A build that cannot reach the mirror still ships a page; it just lists
      // the whole catalog instead of what this release has.
    }
  }
  return null;
}

export async function downloadGroups(lang: 'en' | 'zh'): Promise<DownloadGroup[]> {
  const live = await liveFiles();
  const groups: DownloadGroup[] = [];

  for (const entry of catalog) {
    if (live && !live.has(entry.key)) continue;
    const kind = kinds[entry.kind];
    const file: DownloadFile = {
      key: entry.key,
      label: entry.label?.[lang] ?? kind[lang],
      ext: kind.ext,
      kind: archLabel(entry.platform, entry.arch),
    };
    const group = groups.find((candidate) => candidate.icon === entry.platform);
    if (group) group.files.push(file);
    else groups.push({ platform: download[lang].platformLabels[entry.platform], icon: entry.platform, files: [file] });
  }

  return groups;
}

/** Resolve a file to its mirrored URL; the page never builds URLs itself. */
export function downloadHref(file: DownloadFile): string {
  return `${site.download}/releases/latest/${file.key}`;
}
