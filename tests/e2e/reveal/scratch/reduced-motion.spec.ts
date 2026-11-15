import { expect, test } from '@playwright/test';
import { nameEl, openScratchPage, startScratch } from './harness';

test('prefers-reduced-motion : la carte est remplacée par un fondu, la cible reste affichée', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const h = await openScratchPage(page, { boot: false });
  await startScratch(page);
  await expect(page.locator('[data-reveal]')).toHaveAttribute('data-motion', 'reduced');
  await expect(page.locator('.scratch-canvas')).toHaveCount(0);
  await page.locator('[data-role="stage"] button.reveal-open').click();
  await expect(nameEl(page)).toHaveText('Léa');
  const result = page.locator('[data-role="result"]');
  await expect.poll(() => result.evaluate((el) => getComputedStyle(el).opacity)).toBe('1');
  expect(await result.evaluate((el) => getComputedStyle(el).transitionProperty)).toBe('opacity');
  expect(await page.locator('.reveal-confetti-piece').count()).toBe(0);
  await expect(nameEl(page)).toBeVisible();
  await expect.poll(() => h.confirms.length).toBe(1);
});

test('les transitions de la carte sont coupées avec prefers-reduced-motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openScratchPage(page, { boot: false });
  const t = await page.evaluate(() => {
    const el = document.createElement('div');
    el.className = 'scratch-prompt';
    document.body.appendChild(el);
    return getComputedStyle(el).transitionDuration;
  });
  expect(t).toBe('0s');
});
