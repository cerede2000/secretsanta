import { expect, test } from '@playwright/test';
import { openHarness } from './harness';

const state = (page: import('@playwright/test').Page) =>
  page.evaluate(() => {
    const snow = document.querySelector('.snow');
    const layer = document.querySelector('.snow-layer');
    return {
      exists: !!snow,
      display: snow ? getComputedStyle(snow).display : 'absent',
      animation: layer ? getComputedStyle(layer).animationName : 'absent',
      layers: document.querySelectorAll('.snow-layer').length,
    };
  });

test('la neige tombe par defaut avec un nombre de calques borne', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openHarness(page);
  const s = await state(page);
  expect(s.display).toBe('block');
  expect(s.animation).not.toBe('none');
  expect(s.layers).toBeLessThanOrEqual(5);
});

test('la neige est coupee avec prefers-reduced-motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openHarness(page);
  const s = await state(page);
  expect(s.display === 'none' || s.display === 'absent').toBe(true);
  expect(s.animation === 'none' || s.animation === 'absent').toBe(true);
});

test('la neige s\'arrete quand la preference change en cours de route', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openHarness(page);
  expect((await state(page)).display).toBe('block');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect.poll(async () => (await state(page)).display).toBe('none');
});
