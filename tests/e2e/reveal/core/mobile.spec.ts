import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { ONE, openGift, openReveal, root } from './harness';

test('la coque de révélation (une cible) : ni défilement horizontal ni cible tactile trop petite', async ({ page }) => {
  await openReveal(page, { targets: ONE, seed: 1, letterUrl: '/lettre' });
  await expect(root(page)).toHaveAttribute('data-phase', 'ready');
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);

  await openGift(page);
  await expect(page.locator('[data-role="name"]')).toBeVisible();
  await expect(page.locator('[data-role="letter"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  await expect(page.locator('a.btn[data-role="letter"]')).toBeVisible();
  const box = await page.locator('[data-role="letter"]').boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(44);
});

test('un nom très long reste dans l\'écran', async ({ page }) => {
  await openReveal(page, {
    targets: [{ id: 'x', name: 'Maximilienne-Alexandrine de Montmorency-Saint-Aignan', hint: 'Indice '.repeat(30) }],
    seed: 2,
  });
  await openGift(page);
  await expect(page.locator('[data-role="name"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
});
