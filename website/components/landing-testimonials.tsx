import type { CSSProperties } from 'react';
import type { LandingContent } from '@/lib/landing-content';
import styles from './landing-testimonials.module.css';

type Testimonial = LandingContent['testimonials'][number];

function Card({ testimonial }: { testimonial: Testimonial }) {
  return (
    <figure className={styles.card}>
      <div className="flex items-center gap-3">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={testimonial.avatar}
          alt=""
          width={42}
          height={42}
          decoding="async"
          className="size-[42px] shrink-0 rounded-full border bg-fd-secondary object-cover"
        />
        <span className="text-[15px] font-medium leading-tight">{testimonial.author}</span>
      </div>
      <blockquote className="mt-3.5 text-sm leading-relaxed text-fd-muted-foreground">
        “{testimonial.quote}”
      </blockquote>
    </figure>
  );
}

function Row({
  items,
  direction,
  duration,
  label,
}: {
  items: Testimonial[];
  direction: 'left' | 'right';
  duration: number;
  label: string;
}) {
  // The clone is what makes the loop seamless; it is hidden from assistive
  // technology so every reply is announced exactly once.
  const group = (hidden: boolean) => (
    <div className={styles.group} aria-hidden={hidden || undefined}>
      {items.map((testimonial) => (
        <Card key={testimonial.post} testimonial={testimonial} />
      ))}
    </div>
  );

  return (
    <div
      className={styles.marquee}
      data-direction={direction}
      style={{ '--marquee-duration': `${duration}s` } as CSSProperties}
      tabIndex={0}
      role="group"
      aria-label={label}
    >
      <div className={styles.track}>
        {group(false)}
        {group(true)}
      </div>
    </div>
  );
}

export function LandingTestimonials({
  title,
  items,
  label,
}: {
  title: string;
  items: Testimonial[];
  label: string;
}) {
  const mid = Math.ceil(items.length / 2);

  return (
    <section className="mx-auto max-w-6xl px-4 py-16">
      <h2 className="mb-10 text-center text-2xl font-semibold tracking-tight">{title}</h2>
      <div className={styles.wall}>
        <Row items={items.slice(0, mid)} direction="left" duration={84} label={label} />
        <Row items={items.slice(mid)} direction="right" duration={98} label={label} />
      </div>
    </section>
  );
}
