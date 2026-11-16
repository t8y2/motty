import { existsSync, readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { lastCommitDate } from '@/lib/dates';

/**
 * The changelog is built from `docs/releases/*.md`, the same files the release
 * workflow publishes as the GitHub release body. There is no second copy in the
 * website: a release only has to write its notes once, and the page follows.
 *
 * `docs/releases/<version>.en.md` is optional. It carries the same sections in
 * English for the English changelog; without it the English page shows the
 * Chinese notes rather than a guess at a translation.
 */
export type ChangelogText = { en: string; zh: string };

export type ChangelogSection = {
  title: ChangelogText;
  /** Each language keeps its own list: a missing translation never shows up
   *  as Chinese text on the English page. */
  items: { en: string[]; zh: string[] };
};

export type ChangelogEntry = {
  /** Version without the v; the release tag is `v${version}`. */
  version: string;
  /** Publish date when GitHub knows it, otherwise the notes' commit date. */
  date: string;
  sections: ChangelogSection[];
};

type CanonicalSection = {
  key: 'new' | 'improvements' | 'fixes';
  zh: string;
  en: string;
  names: string[];
};

const canonicalSections: CanonicalSection[] = [
  { key: 'new', zh: '新功能', en: 'New', names: ['新功能', 'New'] },
  { key: 'improvements', zh: '改进', en: 'Improvements', names: ['改进', 'Improved', 'Improvements'] },
  { key: 'fixes', zh: '修复', en: 'Fixes', names: ['修复', 'Fixed', 'Fixes'] },
];

/** Sections the release notes carry but the changelog leaves out. */
const ignoredSections = new Set(['安装', 'Install', 'Installation', 'Packaging']);

type LoadedNotes = {
  version: string;
  zhFile: string;
  enFile?: string;
};

const versionFile = /^(\d+\.\d+\.\d+)\.md$/;

function releasesDirectory(): string {
  const candidates = [
    path.join(process.cwd(), '..', 'docs', 'releases'),
    path.join(process.cwd(), 'docs', 'releases'),
  ];
  for (const candidate of candidates) {
    if (existsSync(candidate)) return candidate;
  }
  throw new Error(`no docs/releases directory below ${process.cwd()}`);
}

function compareVersions(a: string, b: string): number {
  const left = a.split('.').map(Number);
  const right = b.split('.').map(Number);
  for (let index = 0; index < 3; index += 1) {
    if (left[index] !== right[index]) return left[index] - right[index];
  }
  return 0;
}

let cachedNotes: LoadedNotes[] | undefined;

function releaseNotes(): LoadedNotes[] {
  if (cachedNotes) return cachedNotes;
  const directory = releasesDirectory();
  const versions = readdirSync(directory)
    .map((name) => versionFile.exec(name)?.[1])
    .filter((version): version is string => Boolean(version));
  cachedNotes = versions
    .sort((a, b) => compareVersions(b, a))
    .map((version) => {
      const zhFile = path.join(directory, `${version}.md`);
      const enFile = path.join(directory, `${version}.en.md`);
      return { version, zhFile, enFile: existsSync(enFile) ? enFile : undefined };
    });
  return cachedNotes;
}

type ParsedSections = Map<CanonicalSection['key'], string[]>;

function sectionKey(heading: string): CanonicalSection['key'] | undefined {
  const trimmed = heading.trim();
  if (ignoredSections.has(trimmed)) return undefined;
  return canonicalSections.find((section) => section.names.includes(trimmed))?.key;
}

function parseSections(body: string): ParsedSections {
  const sections: ParsedSections = new Map();
  let current: CanonicalSection['key'] | undefined;
  for (const line of body.split('\n')) {
    const heading = /^##\s+(.+?)\s*$/.exec(line);
    if (heading) {
      current = sectionKey(heading[1]);
      if (current && !sections.has(current)) sections.set(current, []);
      continue;
    }
    if (!current) continue;
    const item = /^-\s+(.+?)\s*$/.exec(line);
    if (item) sections.get(current)?.push(item[1]);
  }
  return sections;
}

function cachedRead(file: string): string {
  return readFileSync(file, 'utf8');
}

function entryFromNotes(notes: LoadedNotes, dates: Map<string, string>): ChangelogEntry {
  const zh = parseSections(cachedRead(notes.zhFile));
  const en = notes.enFile ? parseSections(cachedRead(notes.enFile)) : new Map();
  const sections: ChangelogSection[] = [];
  for (const canonical of canonicalSections) {
    const zhItems = zh.get(canonical.key) ?? [];
    const enItems = en.get(canonical.key) ?? [];
    if (zhItems.length === 0 && enItems.length === 0) continue;
    sections.push({
      title: { en: canonical.en, zh: canonical.zh },
      items: { en: enItems.length > 0 ? enItems : zhItems, zh: zhItems },
    });
  }
  return {
    version: notes.version,
    date: dates.get(notes.version) ?? notesDate(notes),
    sections,
  };
}

/** The commit that last touched the notes is a release's date without GitHub. */
function notesDate(notes: LoadedNotes): string {
  return lastCommitDate(path.relative(process.cwd(), notes.zhFile));
}

let cachedDates: Promise<Map<string, string>> | undefined;

/** Publish dates from the GitHub releases; an offline build falls back to git. */
function publishedDates(): Promise<Map<string, string>> {
  if (cachedDates) return cachedDates;
  cachedDates = (async () => {
    const dates = new Map<string, string>();
    try {
      const response = await fetch('https://api.github.com/repos/t8y2/motty/releases?per_page=100', {
        headers: { accept: 'application/vnd.github+json', 'user-agent': 'motty-website-build' },
        signal: AbortSignal.timeout(10_000),
      });
      if (!response.ok) return dates;
      const releases = (await response.json()) as {
        tag_name?: string;
        published_at?: string;
        draft?: boolean;
      }[];
      for (const release of releases) {
        if (!release.tag_name || !release.published_at || release.draft) continue;
        dates.set(release.tag_name.replace(/^v/, ''), release.published_at.slice(0, 10));
      }
    } catch {
      // A build without network keeps working; the notes' commit date stands in.
    }
    return dates;
  })();
  return cachedDates;
}

let cachedEntries: Promise<ChangelogEntry[]> | undefined;

/** Every release, newest first, straight from the release notes. */
export function loadReleases(): Promise<ChangelogEntry[]> {
  if (!cachedEntries) {
    cachedEntries = publishedDates().then((dates) =>
      releaseNotes().map((notes) => entryFromNotes(notes, dates)),
    );
  }
  return cachedEntries;
}

/** The newest release, for the JSON-LD and the sitemap's change date. */
export function latestRelease(): { version: string; date: string } {
  const notes = releaseNotes()[0];
  if (!notes) return { version: '0.0.0', date: lastCommitDate('docs/releases') };
  return { version: notes.version, date: notesDate(notes) };
}
