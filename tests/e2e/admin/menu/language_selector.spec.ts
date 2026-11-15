import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { open } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';

// F10.AC41 : le selecteur de langue est atteignable depuis le menu Plus de
// l'administration, sans defilement horizontal, et change la langue de
// l'interface d'administration.
test('menu Plus : le selecteur de langue change la langue de l\'administration', async ({ page, context }, testInfo) => {
  const g = lane(testInfo).people;
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/plus`);
  const select = page.locator('footer .lang-selector-select');
  const apply = page.locator('footer .lang-selector-apply');
  await select.scrollIntoViewIfNeeded();
  await expect(select).toBeVisible();
  await expectTouchTargets(page.locator('footer .lang-selector-select, footer .lang-selector-apply'));
  await expectNoHorizontalScroll(page);
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
  await expect(page.locator('h1')).toHaveText(/Plus/);

  await select.selectOption('en');
  await Promise.all([page.waitForURL(/admin\/plus/), apply.click()]);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  expect((await context.cookies()).find((c) => c.name === 'lang')?.value).toBe('en');
  // Le menu, la barre basse et les pages suivantes passent en anglais, sans cle brute.
  await expect(page.locator('.admin-menu-item').first()).toContainText('Settings');
  await expect(page.locator('.admin-menu-logout')).toContainText('Sign out');
  await expect(page.locator('.admin-nav-item').first()).toContainText('Dashboard');
  await expectNoHorizontalScroll(page);
  await open(page, `/${g.slug}/admin/participants`);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  const text = await page.locator('main').innerText();
  expect(text, 'cle de catalogue brute').not.toMatch(/\b(admin|core|layout)\.[a-z_]+\.[a-z_.]+\b/);
  expect(text, 'texte francais restant').not.toMatch(/Rechercher|Ajouter quelqu|Participants\n.*Prénom/);
  await expectNoHorizontalScroll(page);

  // Remise en francais.
  await open(page, `/${g.slug}/admin/plus`);
  await page.locator('footer .lang-selector-select').selectOption('fr');
  await Promise.all([page.waitForURL(/admin\/plus/), page.locator('footer .lang-selector-apply').click()]);
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
});
