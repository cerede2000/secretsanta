import { expect, test } from '@playwright/test';
import { ONE, openGift, openReveal } from './harness';

// Compte les contextes audio créés : un son joué en crée au moins un.
async function spyAudio(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    const w = window as any;
    w.__audio = 0;
    const Real = w.AudioContext;
    if (!Real) return;
    w.AudioContext = class extends Real {
      constructor(...a: unknown[]) {
        super(...a);
        w.__audio++;
      }
    };
  });
}
const audio = (page: import('@playwright/test').Page) => page.evaluate(() => (window as any).__audio as number);

test('le son est désactivé par défaut : aucune lecture à la révélation', async ({ page }) => {
  await spyAudio(page);
  await openReveal(page, { targets: ONE, seed: 1 });
  const sw = page.locator('[data-role="sound"]');
  await expect(sw).toHaveAttribute('aria-checked', 'false');
  await openGift(page);
  await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
  await page.waitForTimeout(400);
  expect(await audio(page)).toBe(0);
});

test('le son ne se joue qu\'après activation explicite', async ({ page }) => {
  await spyAudio(page);
  await openReveal(page, { targets: ONE, seed: 1 });
  const sw = page.locator('[data-role="sound"]');
  await sw.click();
  await expect(sw).toHaveAttribute('aria-checked', 'true');
  await openGift(page);
  await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
  await expect.poll(() => audio(page)).toBeGreaterThan(0);
});

test('l\'activation n\'est pas mémorisée : au rechargement le son est de nouveau coupé', async ({ page }) => {
  await spyAudio(page);
  await openReveal(page, { targets: ONE, seed: 1 });
  await page.locator('[data-role="sound"]').click();
  await page.reload();
  await expect(page.locator('[data-role="sound"]')).toHaveAttribute('aria-checked', 'false');
});
