import { expect, test, type Page } from '@playwright/test';
import { seed } from '../../_shared/seed';
import { expectInLowerThird, expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';

// Routes attendues (branchees par l'orchestrateur) : GET /<slug>/admin/plus et
// POST /<slug>/admin/deconnexion, apres la connexion administrateur.
const instancePassword = (): string => seed().instancePassword;

function uniqueSlug(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

async function createAndLogin(page: Page): Promise<string> {
  const slug = uniqueSlug('menu');
  await page.goto('/creer');
  await page.fill('#g-password', instancePassword());
  await page.fill('#g-name', 'Menu test');
  await page.fill('#g-slug', slug);
  await page.click('.groups-submit');
  const secret = ((await page.locator('#admin-secret').textContent()) ?? '').trim();
  await page.goto(`/${slug}/admin/connexion`);
  await page.fill('#access-secret', secret);
  await page.click('.access-submit');
  return slug;
}

test('feuille Plus : cinq entrees, deconnexion separee, cibles tactiles, pas de defilement horizontal', async ({ page }) => {
  const slug = await createAndLogin(page);
  const res = await page.goto(`/${slug}/admin/plus`);
  expect(res?.status()).toBe(200);
  await expect(page.locator('.admin-menu-item')).toHaveCount(5);
  await expect(page.locator('.admin-menu-sensitive .admin-menu-logout')).toBeVisible();
  await expect(page.locator('.admin-nav-item[aria-current="page"]')).toHaveCount(1);
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.admin-menu-item'));
  await expectTouchTargets(page.locator('.admin-nav-item'));
  // La barre basse est atteignable au pouce.
  await expectInLowerThird(page, page.locator('.admin-nav-item[aria-current="page"]'));
});

test('feuille Plus : la deconnexion ferme la session sans boite du navigateur', async ({ page }) => {
  let dialogs = 0;
  const onDialog = async (d: { dismiss(): Promise<void> }) => {
    dialogs += 1;
    await d.dismiss();
  };
  page.on('dialog', onDialog);
  const slug = await createAndLogin(page);
  await page.goto(`/${slug}/admin/plus`);
  await page.click('.admin-menu-logout');
  await expect(page).toHaveURL(new RegExp(`/${slug}/admin/connexion$`));
  await page.goto(`/${slug}/admin/plus`);
  await expect(page).toHaveURL(new RegExp(`/${slug}/admin/connexion$`));
  page.off('dialog', onDialog);
  expect(dialogs).toBe(0);
});
