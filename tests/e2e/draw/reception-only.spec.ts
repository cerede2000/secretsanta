import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';
import { loginWithCode, slug } from './login';

// Code de connexion d'une personne au role reception_seule (tirage fait ou non).
const code = process.env.E2E_DRAW_RECEPTION_CODE ?? '';

test.beforeEach(() => {
  test.skip(code === '', 'E2E_DRAW_RECEPTION_CODE non fourni');
});

test('reception seule : accueil sans cible ni compte a rebours, lettre a ecrire, mobile', async ({ page }) => {
  await loginWithCode(page, code);
  await expect(page.locator('.draw-reception')).toBeVisible();
  await expect(page.locator('[data-countdown]')).toHaveCount(0);
  await expect(page.locator('.draw-pref')).toHaveCount(0);
  await expect(page.locator(`a[href="/${slug}/decouverte"]`)).toHaveCount(0);
  await expect(page.locator(`a[href="/${slug}/lettre"]`).first()).toBeVisible();
  expect(await page.locator('[style]').count()).toBe(0);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  for (const a of await page.locator('.draw-actions a').all()) {
    const box = await a.boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(43.5);
  }
});
