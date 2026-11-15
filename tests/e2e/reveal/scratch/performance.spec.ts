import fs from 'node:fs';
import { expect, test } from '@playwright/test';
import { SCRATCH_CSS, nameEl, openScratch, scratchRows } from './harness';

test('les animations de la carte n\'utilisent que transform et opacity', () => {
  const css = fs.readFileSync(SCRATCH_CSS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
  const allowed = new Set(['transform', 'opacity', 'none']);
  for (const m of css.matchAll(/transition\s*:\s*([^;}]+)/g)) {
    for (const part of m[1].split(',')) {
      const prop = part.trim().split(/\s+/)[0];
      expect(allowed.has(prop), `transition sur ${prop}`).toBe(true);
    }
  }
  expect(css).not.toMatch(/@keyframes/);
  expect(css).not.toMatch(/transition\s*:\s*all/);
});

test('grattage et révélation à 30 images/s ou plus avec ralentissement CPU x4', async ({ page }) => {
  await openScratch(page);
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
  await scratchRows(page, 0, 1);
  await expect(nameEl(page)).toBeVisible();
  await page.waitForTimeout(2500);
  const fps = await page.evaluate(() => {
    const f: number[] = (window as any).__frames;
    cancelAnimationFrame((window as any).__raf);
    return ((f.length - 1) * 1000) / (f[f.length - 1] - f[0]);
  });
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 1 });
  expect(fps).toBeGreaterThanOrEqual(30);
});
