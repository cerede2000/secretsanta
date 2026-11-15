import { expect, test } from '@playwright/test';
import { TWO, openGift, openReveal, root } from './harness';

test('deux cibles avec prefers-reduced-motion : deux fondus, les deux cibles restent affichées', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const h = await openReveal(page, { targets: TWO, seed: 2 });
  const result = page.locator('[data-role="result"]');
  const name = page.locator('[data-role="name"]');

  for (const [i, who] of ['Léa', 'Noam'].entries()) {
    await expect(root(page)).toHaveAttribute('data-motion', 'reduced');
    await openGift(page);
    await expect(name).toHaveText(who);
    // Fondu : opacité seule, aucune animation de confettis ni de lueur.
    await expect.poll(() => result.evaluate((el) => getComputedStyle(el).opacity)).toBe('1');
    const fx = await page.evaluate(() => ({
      confetti: document.querySelectorAll('.reveal-confetti-piece').length,
      glow: getComputedStyle(document.querySelector('.reveal-glow')!).animationName,
      transform: getComputedStyle(document.querySelector('[data-role="result"]')!).transform,
      transition: getComputedStyle(document.querySelector('[data-role="result"]')!).transitionProperty,
    }));
    expect(fx.confetti).toBe(0);
    expect(fx.glow).toBe('none');
    expect(fx.transform).toBe('none');
    expect(fx.transition).toBe('opacity');
    await expect(name).toBeVisible();
    await expect.poll(() => h.confirms.length).toBe(i + 1);
    if (i === 0) await page.locator('[data-role="next"]').click();
  }
  await expect(name).toHaveText('Noam');
});

test('sans prefers-reduced-motion, la révélation lance les confettis', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openReveal(page, { targets: TWO, seed: 2 });
  await expect(root(page)).toHaveAttribute('data-motion', 'full');
  await openGift(page);
  await expect.poll(() => page.locator('.reveal-confetti-piece').count()).toBeGreaterThan(0);
});
