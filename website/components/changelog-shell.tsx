'use client';

import { useEffect, useState, type ReactNode } from 'react';
import { Tag } from 'lucide-react';

export type ChangelogVersionLink = { id: string; label: string };

export type ChangelogToc = {
  /** The id of the release section this contents belongs to. */
  id: string;
  title: string;
  entries: { id: string; label: string }[];
};

/**
 * The three columns of the changelog page: every release on the left, the
 * releases themselves in the middle, the contents of the release in view on
 * the right. Which release is "in view" is the only state, and it is read from
 * the page instead of passed down, so the releases stay server-rendered.
 */
export function ChangelogShell({
  versions,
  toc,
  versionsLabel,
  tocSubtitle,
  children,
}: {
  versions: ChangelogVersionLink[];
  toc: ChangelogToc[];
  versionsLabel: string;
  tocSubtitle: string;
  children: ReactNode;
}) {
  const [activeId, setActiveId] = useState(versions[0]?.id ?? '');

  useEffect(() => {
    const releases = Array.from(document.querySelectorAll<HTMLElement>('[data-changelog-release]'));
    if (releases.length === 0) return;

    let frame = 0;
    const measure = () => {
      frame = 0;
      let current = releases[0];
      for (const release of releases) {
        if (release.getBoundingClientRect().top <= 120) current = release;
      }
      const id = current.dataset.changelogRelease;
      if (id) setActiveId(id);
    };
    const onScroll = () => {
      if (frame === 0) frame = requestAnimationFrame(measure);
    };

    measure();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      window.removeEventListener('scroll', onScroll);
      if (frame !== 0) cancelAnimationFrame(frame);
    };
  }, []);

  const activeToc = toc.find((entry) => entry.id === activeId) ?? toc[0];

  return (
    <div className="changelog-shell">
      <aside className="changelog-sidebar changelog-sidebar-left" aria-label={versionsLabel}>
        <div className="changelog-sidebar-inner">
          <div className="changelog-sidebar-title">
            <Tag size={18} strokeWidth={1.8} aria-hidden />
            <span>{versionsLabel}</span>
          </div>
          <nav className="changelog-version-list">
            {versions.map((version) => (
              <a
                key={version.id}
                href={`#${version.id}`}
                className={`changelog-version-link${version.id === activeId ? ' is-active' : ''}`}
                aria-current={version.id === activeId ? 'true' : undefined}
              >
                <span>{version.label}</span>
              </a>
            ))}
          </nav>
        </div>
      </aside>

      <div className="changelog-content">{children}</div>

      {activeToc && activeToc.entries.length > 0 && (
        <aside className="changelog-sidebar changelog-sidebar-right" aria-label={activeToc.title}>
          <div className="changelog-sidebar-inner">
            <p className="changelog-toc-title">{activeToc.title}</p>
            <p className="changelog-toc-subtitle">{tocSubtitle}</p>
            <nav className="changelog-toc-list">
              {activeToc.entries.map((entry) => (
                <a key={entry.id} href={`#${entry.id}`}>
                  {entry.label}
                </a>
              ))}
            </nav>
          </div>
        </aside>
      )}
    </div>
  );
}
