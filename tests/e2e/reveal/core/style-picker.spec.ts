import { expect, test } from '@playwright/test';
import { ONE, openReveal, root, startManual } from './harness';

test('300 tirages à graine forcée : trois styles vus, jamais deux fois le même d\'affilée', async ({ page }) => {
  await openReveal(page, { boot: false });
  const r = await page.evaluate(async () => {
    const { pickStyle, seededRandom, STYLES } = await import('/static/js/reveal/core/styles.js');
    const out: Record<string, unknown> = { styles: [...STYLES] };
    for (const seed of [1, 42, 2026]) {
      const rand = seededRandom(seed);
      let last: string | null = null;
      const seen = new Set<string>();
      let repeats = 0;
      for (let i = 0; i < 300; i++) {
        const s = pickStyle(last, rand);
        if (s === last) repeats++;
        seen.add(s);
        last = s;
      }
      out['seed' + seed] = { seen: [...seen].sort(), repeats };
    }
    // Aléas extrêmes : jamais le dernier style, même à 0 ou presque 1.
    const extreme: string[] = [];
    for (const last of [null, ...STYLES]) {
      for (const v of [0, 0.5, 0.999999]) {
        const s = pickStyle(last, () => v);
        if (s === last || !STYLES.includes(s)) extreme.push(`${last}/${v}->${s}`);
      }
    }
    out.extreme = extreme;
    return out;
  });
  for (const seed of [1, 42, 2026]) {
    const o = (r as any)['seed' + seed];
    expect(o.seen).toEqual(['envelope', 'gift', 'scratch']);
    expect(o.repeats).toBe(0);
  }
  expect(r.extreme).toEqual([]);
});

test('un nouveau tirage de révélation choisit un autre style', async ({ page }) => {
  await openReveal(page, { boot: false });
  await startManual(page, { targets: ONE, seed: 7 });
  let previous = await root(page).getAttribute('data-style');
  expect(['gift', 'envelope', 'scratch']).toContain(previous);
  for (let i = 0; i < 30; i++) {
    await page.evaluate(() => (window as any).__reveal.rearm());
    const now = await root(page).getAttribute('data-style');
    expect(now, `réarmement ${i + 1}`).not.toBe(previous);
    previous = now;
  }
});

test('le dernier style vu fourni par la source n\'est jamais retiré en premier', async ({ page }) => {
  await openReveal(page, { boot: false });
  for (const last of ['gift', 'envelope', 'scratch']) {
    for (const seed of [1, 2, 3, 4, 5, 6]) {
      await startManual(page, { targets: ONE, seed, last });
      expect(await root(page).getAttribute('data-style')).not.toBe(last);
    }
  }
});

test('le style tiré est mémorisé, sans bloquer si le stockage est indisponible', async ({ page }) => {
  await openReveal(page, { boot: false });
  await startManual(page, { targets: ONE, seed: 3 });
  const style = await root(page).getAttribute('data-style');
  expect(await page.evaluate(() => (window as any).__remembered)).toEqual([style]);
  const ok = await page.evaluate(async () => {
    const { createStyleMemory } = await import('/static/js/reveal/core/memory.js');
    const boom = () => {
      throw new Error('stockage indisponible');
    };
    const m = createStyleMemory({ localStorage: { getItem: boom, setItem: boom } } as any);
    const before = m.get();
    m.set('scratch');
    return { before, after: m.get() };
  });
  expect(ok).toEqual({ before: null, after: 'scratch' });
});
