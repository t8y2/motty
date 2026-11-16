'use client';

import { useEffect, useRef, type CSSProperties } from 'react';
import type { ShowcaseContent } from '@/lib/landing-content';
import styles from './landing-showcase.module.css';

// The first step is a theme reveal from the title-bar button; every later step
// is a different module, pushed in from the right. The captures only have to
// share a size and aspect ratio, not their layout: a push moves whole screens.
const reveals = ['circle', 'push', 'push', 'push', 'push'] as const;

const CAPTURE_WIDTH = 3120;
const CAPTURE_HEIGHT = 1950;

export function LandingShowcase({ content }: { content: ShowcaseContent }) {
  const sectionRef = useRef<HTMLElement>(null);
  const stageRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const section = sectionRef.current;
    const stage = stageRef.current;
    if (!section || !stage) return;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

    const steps = Array.from(section.querySelectorAll<HTMLElement>('[data-showcase-step]'));
    const captions = Array.from(section.querySelectorAll<HTMLElement>('[data-showcase-caption]'));
    let nav = 56;
    let frame = 0;

    const clamp = (value: number) => Math.min(1, Math.max(0, value));

    const measure = () => {
      const header = document.querySelector('header');
      nav = header ? header.getBoundingClientRect().height : 56;
      section.style.setProperty('--showcase-nav', `${nav}px`);
    };

    const update = () => {
      frame = 0;
      const top = section.getBoundingClientRect().top;
      // The distance from the section's place in the document to the line the
      // window pins on. enter is 0 when the section arrives and 1 once it is
      // pinned; the transform stands the window up over that distance.
      const rise = top + window.scrollY - nav;
      const enter = rise > 0 ? clamp(1 - (top - nav) / rise) : 1;
      section.style.setProperty('--showcase-enter', enter.toFixed(4));

      // Every step owns an equal share of the scroll while pinned, and changes
      // in the middle 60% of its share. Scrolling back plays it backwards.
      const travel = section.offsetHeight - stage.offsetHeight;
      const story = travel > 0 ? clamp((nav - top) / travel) * steps.length : 0;
      let current = -1;
      for (let i = 0; i < steps.length; i++) {
        let t = clamp((story - i - 0.2) / 0.6);
        t = t * t * (3 - 2 * t);
        steps[i].style.setProperty('--showcase-t', t.toFixed(4));
        steps[i].toggleAttribute('data-on', t > 0);
        if (t >= 0.5) current = i;
      }
      for (let i = 0; i < captions.length; i++) {
        captions[i].toggleAttribute('data-current', i === current);
      }
    };

    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    const remeasure = () => {
      measure();
      schedule();
    };

    section.setAttribute('data-live', '');
    measure();
    update();
    window.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', remeasure);
    window.addEventListener('load', remeasure);

    return () => {
      window.removeEventListener('scroll', schedule);
      window.removeEventListener('resize', remeasure);
      window.removeEventListener('load', remeasure);
      if (frame) cancelAnimationFrame(frame);
      section.removeAttribute('data-live');
    };
  }, []);

  return (
    <section
      ref={sectionRef}
      className={styles.section}
      style={{ '--showcase-steps': content.steps.length } as CSSProperties}
    >
      <div ref={stageRef} className={styles.stage}>
        <div className={styles.window}>
          <div className={styles.screen}>
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={content.base.image}
              width={CAPTURE_WIDTH}
              height={CAPTURE_HEIGHT}
              alt={content.base.alt}
              fetchPriority="high"
            />
            {content.steps.map((step, index) => {
              const reveal = reveals[index];
              if (!reveal) return null;
              const previous = index === 0 ? content.base.image : content.steps[index - 1].image;
              return (
                <div
                  key={`${step.image}-${index}`}
                  className={styles.step}
                  data-showcase-step
                  data-reveal={reveal}
                  aria-hidden
                >
                  <StepLayers reveal={reveal} image={step.image} previous={previous} />
                </div>
              );
            })}
          </div>
        </div>
        <ol className={styles.captions}>
          {content.steps.map((step) => (
            <li key={step.title} data-showcase-caption>
              <strong>{step.title}</strong>
              <span>{step.body}</span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}

function StepLayers({
  reveal,
  image,
  previous,
}: {
  reveal: 'circle' | 'push';
  image: string;
  previous: string;
}) {
  const next = <LayerImage src={image} />;

  if (reveal === 'circle') {
    return <div className={`${styles.layer} ${styles.reveal}`}>{next}</div>;
  }

  // Whole screens: the old one leaves to the left as the new one arrives from
  // the right. Both fill the window, so the previous state never shows through.
  return (
    <>
      <div className={`${styles.layer} ${styles.pushOld}`}>
        <LayerImage src={previous} />
      </div>
      <div className={`${styles.layer} ${styles.pushNew}`}>{next}</div>
    </>
  );
}

function LayerImage({ src }: { src: string }) {
  return (
    /* eslint-disable-next-line @next/next/no-img-element */
    <img src={src} width={CAPTURE_WIDTH} height={CAPTURE_HEIGHT} alt="" loading="lazy" decoding="async" />
  );
}
