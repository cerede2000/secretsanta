import { expect, test } from '@playwright/test';
import { nameEl, openGiftPage, startGift } from './harness';

test('prefers-reduced-motion : le cadeau est remplacé par un fondu, la cible reste affichée', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const h = await openGiftPage(page, { boot: false });
  await startGift(page);
  await expect(page.locator('[data-reveal]')).toHaveAttribute('data-motion', 'reduced');
  // Le style animé n'est pas monté : seul le bouton équivalent est proposé.
  await expect(page.locator('.gift-art')).toHaveCount(0);
  await page.locator('[data-role="stage"] button.reveal-open').click();
  await expect(nameEl(page)).toHaveText('Léa');
  const result = page.locator('[data-role="result"]');
  await expect.poll(() => result.evaluate((el) => getComputedStyle(el).opacity)).toBe('1');
  expect(await result.evaluate((el) => getComputedStyle(el).transitionProperty)).toBe('opacity');
  expect(await page.locator('.reveal-confetti-piece').count()).toBe(0);
  await expect(nameEl(page)).toBeVisible();
  await expect.poll(() => h.confirms.length).toBe(1);
});

test('les transitions du cadeau sont coupées avec prefers-reduced-motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openGiftPage(page, { boot: false });
  const t = await page.evaluate(() => {
    const el = document.createElement('div');
    el.className = 'gift-ribbon';
    document.body.appendChild(el);
    return getComputedStyle(el).transitionDuration;
  });
  expect(t).toBe('0s');
});
