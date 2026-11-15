import { expect, test } from '@playwright/test';
import { nameEl, openEnvelopePage, startEnvelope } from './harness';

test('prefers-reduced-motion : l\'enveloppe est remplacée par un fondu, la cible reste affichée', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  const h = await openEnvelopePage(page, { boot: false });
  await startEnvelope(page);
  await expect(page.locator('[data-reveal]')).toHaveAttribute('data-motion', 'reduced');
  // Le style animé n'est pas monté : seul le bouton équivalent est proposé.
  await expect(page.locator('.envelope')).toHaveCount(0);
  await expect(page.locator('.envelope-svg')).toHaveCount(0);
  await page.locator('[data-role="stage"] button.reveal-open').click();
  await expect(nameEl(page)).toHaveText('Léa');
  const result = page.locator('[data-role="result"]');
  await expect.poll(() => result.evaluate((el) => getComputedStyle(el).opacity)).toBe('1');
  expect(await result.evaluate((el) => getComputedStyle(el).transitionProperty)).toBe('opacity');
  expect(await page.locator('.reveal-confetti-piece').count()).toBe(0);
  await expect(nameEl(page)).toBeVisible();
  await expect.poll(() => h.confirms.length).toBe(1);
});

test('les transitions de l\'enveloppe sont coupées avec prefers-reduced-motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openEnvelopePage(page, { boot: false });
  const durations = await page.evaluate(() =>
    ['envelope-letter', 'envelope-flap', 'envelope-seal-l', 'envelope-seal-r', 'envelope-star'].map((cls) => {
      const el = document.createElement('div');
      el.className = cls;
      document.body.appendChild(el);
      return getComputedStyle(el).transitionDuration;
    }),
  );
  for (const d of durations) expect(d).toBe('0s');
});
