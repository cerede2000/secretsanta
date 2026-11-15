import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../_shared/admin';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { lane, loginAdmin } from '../_shared/seed';

// F10b.AC9 (part de l'exploitation) : la page « Données » (/{slug}/admin/donnees)
// n'a aucun défilement horizontal et toutes ses zones tactiles mesurent au moins
// 44 px en 360x640 et 390x844, y compris la case « purger » ; la feuille « Plus »
// propose l'entrée « Données ».

test('page Données : ni défilement horizontal ni cible de moins de 44 px', async ({ page }, testInfo) => {
  const group = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, group);
  await open(page, `/${group.slug}/admin/donnees`);

  // Les trois blocs sont là : données, accès, zone sensible.
  await expect(page.locator('#adminops-data')).toBeVisible();
  await expect(page.locator('#adminops-access')).toBeVisible();
  await expect(page.locator('#adminops-danger')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);

  // Les boutons et liens d'action (export, import, sauvegarde, accès, suppression).
  await expectTouchTargets(page.locator('.admin-menu-body a.btn, .admin-menu-body button'));

  // La case « purger » se touche par toute sa ligne.
  const purge = page.locator('label.adminops-check');
  await expect(purge).toBeVisible();
  await expectTouchTargets(purge);
  await expect(purge.locator('input[type="checkbox"][name="purge"]')).not.toBeChecked();
  await purge.click();
  await expect(purge.locator('input[type="checkbox"]')).toBeChecked();
  await expectNoHorizontalScroll(page);
});

test('la feuille Plus propose « Données » vers /admin/donnees', async ({ page }, testInfo) => {
  const group = lane(testInfo).people;
  await loginAdmin(page, group);
  await open(page, `/${group.slug}/admin/plus`);
  const entry = page.locator(`a.admin-menu-item[href="/${group.slug}/admin/donnees"]`);
  await expect(entry).toHaveCount(1);
  await expect(entry).toContainText('Données');
  await expectCleanAdminPage(page);
  await entry.click();
  await expect(page).toHaveURL(new RegExp(`/${group.slug}/admin/donnees$`));
  await expect(page.locator('#adminops-title')).toBeVisible();
});

test('page Données : erreur d\'import sous le champ, sans défilement ni petite cible', async ({ page }, testInfo) => {
  const group = lane(testInfo).people;
  await loginAdmin(page, group);
  await open(page, `/${group.slug}/admin/donnees`);
  // Un fichier qui n'est pas du JSON valide : le message reste sous le champ.
  await page.locator('#adminops-file').setInputFiles({ name: 'cassé.json', mimeType: 'application/json', buffer: Buffer.from('pas du json') });
  await page.locator('form[action$="/participants/import"] button[type="submit"]').click();
  await expect(page.locator('#adminops-file-error[role="alert"]')).toBeVisible();
  await expectCleanAdminPage(page);
});
