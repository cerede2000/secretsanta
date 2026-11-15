import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../_shared/admin';
import { expectNoHorizontalScroll } from '../_shared/mobile';
import { lane, loginAdmin } from '../_shared/seed';

// F6c.AC13 : la page « Délivrabilité » (/{slug}/admin/courrier/delivrabilite) n'a
// aucun défilement horizontal et toutes ses zones tactiles mesurent au moins
// 44 px en 360x640 et 390x844 ; aucune boîte du navigateur, aucun style en ligne.

test('page Délivrabilité : ni défilement horizontal ni cible de moins de 44 px', async ({ page }, testInfo) => {
  const group = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, group);
  await open(page, `/${group.slug}/admin/courrier/delivrabilite`);

  await expect(page.locator('#maildiag-title')).toBeVisible();
  await expect(page.locator('#maildiag-report')).toBeVisible();
  await expect(page.locator('#maildiag-checks')).toBeVisible();
  await expect(page.locator('#maildiag-test')).toBeVisible();
  await expect(page.locator('#maildiag-unverifiable')).toBeVisible();
  await expect(page.locator('#maildiag-suppression-title')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);

  // Le bandeau vert occupe toute la largeur.
  const head = await page.locator('.maildiag-head').boundingBox();
  const width = page.viewportSize()!.width;
  expect(head!.x).toBeLessThanOrEqual(0.5);
  expect(head!.width).toBeGreaterThanOrEqual(width - 0.5);
  await expectNoHorizontalScroll(page);
});

test('page Délivrabilité : une adresse invalide s\'affiche sous le champ', async ({ page }, testInfo) => {
  const group = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, group);
  await open(page, `/${group.slug}/admin/courrier/delivrabilite`);
  const form = page.locator(`form[action$="/delivrabilite/test"]`);
  // Le navigateur refuse déjà une adresse mal formée ; on contourne pour atteindre la réponse du serveur.
  await form.locator('input[name="to"]').evaluate((el: HTMLInputElement) => { el.type = 'text'; });
  await form.locator('input[name="to"]').fill('pas une adresse');
  await form.locator('button[type="submit"]').click();
  await expect(page.locator('#maildiag-to-error[role="alert"]')).toBeVisible();
  await expect(page.locator('#maildiag-to')).toHaveAttribute('aria-invalid', 'true');
  await expectCleanAdminPage(page, dialogs);
});

test('page Délivrabilité : le bouton Copier ne déclenche aucune boîte native', async ({ page, context }, testInfo) => {
  const group = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await context.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => undefined);
  await loginAdmin(page, group);
  await open(page, `/${group.slug}/admin/courrier/delivrabilite`);
  const buttons = page.locator('button[data-copy-target]');
  const n = await buttons.count();
  for (let i = 0; i < n; i++) {
    await buttons.nth(i).click();
    await expect(page.locator(`#${await buttons.nth(i).getAttribute('data-copy-status')}`)).toBeVisible();
  }
  expect(dialogs()).toBe(0);
  await expectNoHorizontalScroll(page);
});
