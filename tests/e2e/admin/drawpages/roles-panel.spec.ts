import { expect, test, type Page, type TestInfo } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll, expectTouchTargets } from '../../_shared/mobile';
import { lane, loginAdmin, type Group } from '../../_shared/seed';

// F10.AC29 (panneau « Roles dans le tirage ») : le panneau de la section Tirage
// (/<slug>/admin/tirage) et la page de modification en lot (/<slug>/admin/roles,
// internal/admin/roles) n'ont aucun defilement horizontal et gardent des cibles
// de 44 px. Donnees : groupe « roles » du couloir, cinq personnes au role normal
// et aucun tirage ; chaque test remet les roles a « Normal » (_shared/seed.ts). Les
// tests s'executent en parallele sur le meme groupe : le lot ne touche que les
// deux premieres lignes, le selecteur d'une ligne que la derniere.
async function openPanel(page: Page, testInfo: TestInfo): Promise<Group> {
  const g = lane(testInfo).roles;
  await loginAdmin(page, g);
  const res = await page.goto(`/${g.slug}/admin/roles`);
  expect(res?.status(), `/${g.slug}/admin/roles doit exister`).toBe(200);
  return g;
}

test('section Tirage : recapitulatif en comptes, lien vers la modification en lot, sans debordement', async ({ page }, testInfo) => {
  const g = lane(testInfo).roles;
  await loginAdmin(page, g);
  const res = await page.goto(`/${g.slug}/admin/tirage`);
  expect(res?.status()).toBe(200);
  const section = page.locator('section', { has: page.locator(`a[href="/${g.slug}/admin/roles"]`) }).last();
  await expect(section).toContainText(String(g.people.length));
  // Jamais un nom dans le recapitulatif.
  const text = await section.innerText();
  for (const p of g.people) expect(text).not.toContain(p.firstName);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  await expectTouchTargets(section.locator('a.btn'));
  expect(await page.locator('[style]').count()).toBe(0);
});

test('panneau des roles : aucun defilement horizontal et cibles tactiles de 44 px', async ({ page }, testInfo) => {
  await openPanel(page, testInfo);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  for (const el of await page.locator('.roles select, .roles summary, .roles-pick').all()) {
    if (!(await el.isVisible())) continue;
    const box = await el.boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(43.5);
  }
});

test('panneau des roles : le recapitulatif ne donne que des comptes', async ({ page }, testInfo) => {
  await openPanel(page, testInfo);
  const counts = page.locator('.roles-counts > div');
  await expect(counts).toHaveCount(6);
  for (const dd of await page.locator('.roles-counts dd').all()) {
    expect(await dd.textContent()).toMatch(/^\d+$/);
  }
  await expect(page.locator('.roles-card').first().locator('.chip')).toBeVisible();
});

test('panneau des roles : le lot applique un role a plusieurs personnes', async ({ page }, testInfo) => {
  page.on('dialog', () => {
    throw new Error('boite du navigateur interdite');
  });
  await openPanel(page, testInfo);
  const picks = page.locator('[data-roles-pick]');
  await picks.nth(0).check();
  await picks.nth(1).check();
  await expect(page.locator('[data-roles-count]')).toContainText('2');
  await page.locator('#roles-bulk-role').selectOption('un_seul');
  await page.locator('.roles-bulk-apply').click();
  await expect(page.locator('[role="status"]')).toBeVisible();
  const selects = page.locator('.roles-row .roles-select');
  await expect(selects.nth(0)).toHaveValue('un_seul');
  await expect(selects.nth(1)).toHaveValue('un_seul');
  await expectNoHorizontalScroll(page);
  // remise a l'etat initial
  await picks.nth(0).check();
  await picks.nth(1).check();
  await page.locator('#roles-bulk-role').selectOption('normal');
  await page.locator('.roles-bulk-apply').click();
  await expect(selects.nth(0)).toHaveValue('normal');
});

test('panneau des roles : le selecteur d\'une ligne s\'envoie au changement', async ({ page }, testInfo) => {
  await openPanel(page, testInfo);
  const last = page.locator('.roles-row .roles-select').last();
  await last.selectOption('reception_seule');
  await expect(page.locator('[role="status"]')).toBeVisible();
  await expect(page.locator('.roles-row .roles-select').last()).toHaveValue('reception_seule');
  await page.locator('.roles-row .roles-select').last().selectOption('normal');
  await expect(page.locator('.roles-row .roles-select').last()).toHaveValue('normal');
});

test('panneau des roles : un lot sans personne affiche un message integre sous le champ', async ({ page }, testInfo) => {
  page.on('dialog', () => {
    throw new Error('boite du navigateur interdite');
  });
  await openPanel(page, testInfo);
  await page.locator('.roles-bulk-apply').click();
  const alert = page.locator('#roles-error');
  await expect(alert).toBeVisible();
  await expect(page.locator('#roles-bulk-role')).toHaveAttribute('aria-invalid', 'true');
  await expectNoHorizontalScroll(page);
});
