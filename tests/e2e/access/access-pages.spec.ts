import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';

// Pre-requis : un groupe existe, son slug est dans E2E_GROUP_SLUG (defaut
// « e2e-acces »). Routes attendues (internal/accesspages, Routes()) :
//   GET /<slug>/connexion, GET /<slug>/admin/connexion,
//   GET /<slug>/connexion/lien/<jeton>, GET /<slug>/acces/<jeton>.
const slug = process.env.E2E_GROUP_SLUG ?? 'e2e-acces';

const PAGES: Array<[string, string]> = [
  ['screen-connexion', `/${slug}/connexion`],
  ['screen-admin-connexion', `/${slug}/admin/connexion`],
  ['confirmation du lien magique', `/${slug}/connexion/lien/jeton-de-test`],
  ['confirmation du lien personnel', `/${slug}/acces/jeton-de-test`],
];

for (const [name, path] of PAGES) {
  test(`${name} : aucun defilement horizontal et cibles tactiles de 44 px`, async ({ page }) => {
    const res = await page.goto(path);
    expect(res?.status(), `${path} doit exister (groupe ${slug})`).toBe(200);
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    for (const el of await page.locator('input:not([type="hidden"]), select, summary, .access-tab').all()) {
      if (!(await el.isVisible())) continue;
      const box = await el.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(43.5);
    }
  });
}

test('screen-connexion : le panneau du lien magique s\'ouvre sans defilement horizontal', async ({ page }) => {
  await page.goto(`/${slug}/connexion`);
  await page.locator('summary').click();
  await expect(page.locator('input[name="email"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('screen-connexion : un code faux affiche un message integre, sans boite du navigateur', async ({ page }) => {
  page.on('dialog', () => { throw new Error('boite du navigateur interdite'); });
  await page.goto(`/${slug}/connexion`);
  await page.locator('input[name="code"]').fill('ZZZZZZZZ');
  await page.getByRole('button', { name: /Ouvrir mon tirage/ }).click();
  await expect(page.locator('[role="alert"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
});

test('screen-admin-connexion : onglets, secret faux et message integre', async ({ page }) => {
  await page.goto(`/${slug}/admin/connexion`);
  const tabs = page.locator('[role="tab"]');
  await expect(tabs).toHaveCount(2);
  await tabs.nth(1).click();
  await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'true');
  await page.locator('input[name="secret"]').fill('secret-faux');
  await page.locator('form[data-access-admin] button[type="submit"]').click();
  await expect(page.locator('[role="alert"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('un lien affiche un bouton de confirmation et ne se consomme pas en GET', async ({ page }) => {
  await page.goto(`/${slug}/connexion/lien/jeton-de-test`);
  await expect(page.locator('form[method="post"] button[type="submit"]')).toBeVisible();
  expect(new URL(page.url()).pathname).toBe(`/${slug}/connexion/lien/jeton-de-test`);
});
