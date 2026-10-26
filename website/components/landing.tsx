import Link from 'next/link';
import { Bot, FolderTree, KeyRound, MonitorPlay, Terminal, Waypoints, type LucideIcon } from 'lucide-react';
import { landing } from '@/lib/landing-content';
import { resolveLang } from '@/lib/i18n';

const icons: Record<(typeof landing.en.features)[number]['icon'], LucideIcon> = {
  terminal: Terminal,
  key: KeyRound,
  folder: FolderTree,
  network: Waypoints,
  monitor: MonitorPlay,
  bot: Bot,
};

export function Landing({ lang }: { lang: string }) {
  const l = resolveLang(lang);
  const c = landing[l];

  return (
    <main className="flex-1">
      <section className="relative isolate overflow-hidden px-4 pt-16 pb-12 text-center">
        <span className="mb-6 inline-block rounded-full border bg-fd-secondary px-3.5 py-1.5 text-sm font-medium text-fd-secondary-foreground">
          {c.badge}
        </span>
        <h1 className="mx-auto max-w-3xl text-4xl font-semibold tracking-tight text-balance sm:text-6xl">
          {c.title}
        </h1>
        <p className="mx-auto mt-5 max-w-6xl text-lg text-balance text-fd-muted-foreground">{c.subtitle}</p>
        <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
          <Link
            href={`/${l}/docs/guides/getting-started`}
            className="rounded-lg border bg-fd-background px-6 py-2.5 font-medium transition-colors hover:bg-fd-accent"
          >
            {c.start}
          </Link>
          <Link
            href={`/${l}/docs/guides/install`}
            className="rounded-lg bg-fd-primary px-6 py-2.5 font-medium text-fd-primary-foreground transition-opacity hover:opacity-90"
          >
            {c.install}
          </Link>
        </div>
        <div className="mx-auto mt-12 max-w-7xl rounded-xl border bg-fd-card p-1.5 shadow-xl">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src="/hero-dark.webp"
            width={2800}
            height={1750}
            alt={c.heroAlt}
            className="w-full rounded-lg"
            fetchPriority="high"
          />
        </div>
      </section>

      <section className="relative isolate mx-auto max-w-5xl px-4 py-16">
        <div
          aria-hidden
          className="pointer-events-none absolute -top-16 left-1/2 -z-10 h-64 w-[40rem] -translate-x-1/2 rounded-full bg-fd-primary/10 blur-3xl"
        />
        <h2 className="mb-10 text-center text-2xl font-semibold tracking-tight">{c.featuresTitle}</h2>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {c.features.map((f) => {
            const Icon = icons[f.icon];
            return (
              <div
                key={f.title}
                className="group rounded-2xl border bg-fd-card p-5 shadow-[inset_0_1px_0_rgba(255,255,255,0.05)] transition-colors hover:border-fd-primary/30"
              >
                <div className="mb-4 flex size-10 items-center justify-center rounded-xl bg-fd-primary/10 ring-1 ring-fd-primary/20 transition-colors group-hover:bg-fd-primary/15">
                  <Icon className="size-5 text-fd-primary" />
                </div>
                <h3 className="mb-1.5 font-medium">{f.title}</h3>
                <p className="text-sm leading-relaxed text-fd-muted-foreground">{f.body}</p>
              </div>
            );
          })}
        </div>
      </section>

      <section className="mx-auto max-w-6xl px-4 py-16">
        <h2 className="mb-10 text-center text-2xl font-semibold tracking-tight">{c.screensTitle}</h2>
        <div className="grid gap-14">
          {c.screens.map((s, index) => (
            <div
              key={s.title}
              className={`grid items-center gap-8 lg:gap-12 ${
                index % 2 === 1 ? 'lg:grid-cols-[1fr_1.7fr]' : 'lg:grid-cols-[1.7fr_1fr]'
              }`}
            >
              <figure
                className={`overflow-hidden rounded-2xl border bg-fd-card p-1.5 shadow-xl ${
                  index % 2 === 1 ? 'lg:order-last' : ''
                }`}
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={s.image}
                  width={2400}
                  height={1500}
                  alt={s.alt}
                  loading="lazy"
                  className="w-full rounded-xl"
                />
              </figure>
              <div className="lg:px-4">
                <h3 className="mb-2 text-xl font-semibold tracking-tight">{s.title}</h3>
                <p className="leading-relaxed text-fd-muted-foreground">{s.body}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 py-16">
        <h2 className="mb-10 text-center text-2xl font-semibold tracking-tight">{c.principlesTitle}</h2>
        <div className="grid gap-px overflow-hidden rounded-2xl border bg-fd-border sm:grid-cols-3">
          {c.principles.map((p, index) => (
            <div key={p.title} className="bg-fd-card p-6">
              <span className="mb-3 block font-mono text-xs tracking-widest text-fd-primary/70">
                {String(index + 1).padStart(2, '0')}
              </span>
              <h3 className="mb-1.5 font-medium">{p.title}</h3>
              <p className="text-sm leading-relaxed text-fd-muted-foreground">{p.body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 py-16">
        <h2 className="mb-10 text-center text-2xl font-semibold tracking-tight">{c.faqTitle}</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          {c.faq.map((item) => (
            <div key={item.q} className="rounded-2xl border bg-fd-card p-5">
              <h3 className="mb-1.5 font-medium">{item.q}</h3>
              <p className="text-sm leading-relaxed text-fd-muted-foreground">{item.a}</p>
            </div>
          ))}
        </div>
      </section>

      <footer className="mx-auto max-w-5xl px-4 pb-14">
        <div className="flex flex-col items-center justify-between gap-3 border-t pt-8 text-sm text-fd-muted-foreground sm:flex-row">
          <p>
            {c.footer.copyright} · {c.footer.platforms}
          </p>
          <a
            href="https://github.com/t8y2/motty/blob/main/LICENSE"
            target="_blank"
            rel="noreferrer"
            className="transition-colors hover:text-fd-foreground"
          >
            {c.footer.license}
          </a>
        </div>
      </footer>
    </main>
  );
}
