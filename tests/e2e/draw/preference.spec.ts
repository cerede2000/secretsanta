import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';
import { loginWithCode, slug } from './login';

// Code de connexion d'une personne au role normal, tirage pas encore fait.
const code = process.env.E2E_DRAW_NORMAL_CODE ?? '';

test.beforeEach(() => {
  test.skip(code === '', 'E2E_DRAW_NORMAL_CODE non fourni');
});

test('preference « un seul cadeau » : decochee par defaut, bascule, mobile', async ({ page }) => {
  await loginWithCode(page, code);
  const sw = page.locator('.draw-pref [role="switch"]');
  await expect(sw).toBeVisible();
  const before = await sw.getAttribute('aria-checked');
  expect(before).toBe('false');
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  const box = await sw.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(43.5);
  expect(box!.width).toBeGreaterThanOrEqual(43.5);

  await sw.click();
  await expect(page).toHaveURL(new RegExp(`/${slug}/participant$`));
  await expect(page.locator('.draw-pref [role="switch"]')).toHaveAttribute('aria-checked', 'true');
  await expectNoHorizontalScroll(page);

  // Remise a l'etat initial.
  await page.locator('.draw-pref [role="switch"]').click();
  await expect(page.locator('.draw-pref [role="switch"]')).toHaveAttribute('aria-checked', 'false');
});
