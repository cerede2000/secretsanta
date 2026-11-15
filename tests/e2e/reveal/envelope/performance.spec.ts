import fs from 'node:fs';
import { expect, test } from '@playwright/test';
import { ENVELOPE_CSS, nameEl, openEnvelope, seal } from './harness';

test('les animations de l\'enveloppe n\'utilisent que transform et opacity', () => {
  const css = fs.readFileSync(ENVELOPE_CSS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const allowed = new Set(['transform', 'opacity', 'none']);
  for (const m of css.matchAll(/transition\s*:\s*([^;}]+)/g)) {
    for (const part of m[1].split(/,(?![^(]*\))/)) {
      const prop = part.trim().split(/\s+/)[0];
      expect(allowed.has(prop), `transition sur ${prop}`).toBe(true);
    }
  }
  for (const m of css.matchAll(/will-change\s*:\s*([^;}]+)/g)) {
    for (const part of m[1].split(',')) expect(['transform', 'opacity']).toContain(part.trim());
  }
  expect(css).not.toMatch(/@keyframes/);
  expect(css).not.toMatch(/transition\s*:\s*all/);
});

test('le sceau brisé n\'anime que transform et opacité (styles calculés)', async ({ page }) => {
  await openEnvelope(page);
  const props = await page.evaluate(() => {
    const out = new Set<string>();
    for (const sel of ['.envelope-letter', '.envelope-flap', '.envelope-seal-l', '.envelope-seal-r', '.envelope-star']) {
      getComputedStyle(document.querySelector(sel)!).transitionProperty.split(',').forEach((p) => out.add(p.trim()));
    }
    return [...out];
  });
  expect(props.length).toBeGreaterThan(0);
  for (const p of props) expect(['transform', 'opacity']).toContain(p);
});

test('bris du sceau et révélation à 30 images/s ou plus avec ralentissement CPU x4', async ({ page }) => {
  await openEnvelope(page);
  const cdp = await page.context().newCDPSession(page);
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
  await page.evaluate(() => {
    const w = window as any;
    w.__frames = [];
    const tick = (t: number) => {
      w.__frames.push(t);
      w.__raf = requestAnimationFrame(tick);
    };
    w.__raf = requestAnimationFrame(tick);
  });
  await seal(page).click();
  await expect(nameEl(page)).toBeVisible();
  await page.waitForTimeout(2500);
  const fps = await page.evaluate(() => {
    const f: number[] = (window as any).__frames;
    cancelAnimationFrame((window as any).__raf);
    // Fenêtre complète : de la première image à la dernière.
    return ((f.length - 1) * 1000) / (f[f.length - 1] - f[0]);
  });
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 });
  expect(fps).toBeGreaterThanOrEqual(30);
});
