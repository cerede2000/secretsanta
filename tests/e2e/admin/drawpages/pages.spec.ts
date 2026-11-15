import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';

// F10.AC23 : les pages de confirmation du tirage, de l'ajustement et
// d'historique n'ont aucun defilement horizontal et leurs zones tactiles
// mesurent au moins 44 px en 360x640 et 390x844. Donnees : groupes d'amorcage du
// couloir (_shared/seed.ts) : « people » (aucun tirage), « reg » (inscriptions
// en attente), « waiting » (tirage fait, revelation programmee) et « revealed »
// (tirage fait, revelation ouverte).

test('confirmation du lancement : integree a la page, aucune paire, rien n\'est lance par un simple affichage', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/tirage?confirmer=lancer`);
  const confirm = page.locator('form.drw-confirm[role="group"]');
  await expect(confirm).toBeVisible();
  await expect(confirm.locator('button.btn-danger')).toBeVisible();
  await expect(confirm.locator('a.btn-ghost')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
  // Un GET ne modifie rien : le tirage n'est toujours pas lance apres affichage.
  await open(page, `/${g.slug}/admin/tirage`);
  await expect(page.locator('main')).toContainText('pas encore été lancé');
  // Annuler ramene sur l'onglet Tirage, sans confirmation.
  await open(page, `/${g.slug}/admin/tirage?confirmer=lancer`);
  await confirm.locator('a.btn-ghost').click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/admin/tirage$`));
  await expect(page.locator('form.drw-confirm')).toHaveCount(0);
});

test('confirmation du lancement avec inscriptions en attente : choix explicite ignorer ou supprimer', async ({ page }, testInfo) => {
  const g = lane(testInfo).reg;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/tirage?confirmer=lancer`);
  const pending = page.locator('fieldset.drw-pending');
  await expect(pending).toBeVisible();
  await expect(pending.locator('legend')).toContainText(/\d+/);
  await expect(pending.locator('input[type="radio"]')).toHaveCount(2);
  await expectCleanAdminPage(page, dialogs);
  // Sans choix, le formulaire refuse l'envoi (champ requis) : le tirage n'est pas lance.
  await page.locator('form.drw-confirm button.btn-danger').click();
  await expect(page).toHaveURL(/confirmer=lancer/);
  expect(await pending.locator('input[type="radio"]').first().evaluate((el: HTMLInputElement) => el.validity.valueMissing)).toBe(true);
});

test('confirmation de l\'ouverture immediate : integree a la page', async ({ page }, testInfo) => {
  const g = lane(testInfo).waiting;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/tirage?confirmer=ouvrir`);
  const confirm = page.locator('form.drw-confirm[role="group"]');
  await expect(confirm).toBeVisible();
  await expect(confirm.locator('button.btn-green')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
  // La date de revelation est modifiable tant que la revelation n'est pas ouverte.
  await expect(page.locator('#drw-date')).toBeVisible();
  await expect(page.locator('#drw-date')).not.toHaveValue('');
});

test('onglet Ajuster : avant le tirage, message sans bouton ; apres, ajustement sans nom', async ({ page }, testInfo) => {
  const before = lane(testInfo).people;
  const after = lane(testInfo).revealed;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, before);
  await open(page, `/${before.slug}/admin/tirage?onglet=ajuster`);
  await expect(page.locator('main')).toContainText('pas encore de tirage à ajuster');
  await expectCleanAdminPage(page, dialogs);

  await loginAdmin(page, after);
  await open(page, `/${after.slug}/admin/tirage?onglet=ajuster`);
  await expect(page.locator('main')).toContainText('Aucune paire n\'est jamais affichée');
  const text = await page.locator('main').innerText();
  for (const p of after.people) expect(text, 'aucun nom dans l\'ajustement').not.toContain(p.firstName);
  await expectCleanAdminPage(page, dialogs);
});

test('onglet Historique : par annee, sans paire', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/tirage?onglet=historique`);
  await expect(page.locator('main')).toContainText(String(new Date().getFullYear()));
  await expect(page.locator('main')).toContainText('jamais les paires');
  const text = await page.locator('main').innerText();
  for (const p of g.people) expect(text).not.toContain(p.firstName);
  await expectCleanAdminPage(page, dialogs);
});

test('les trois onglets : segmentes, cibles de 44 px, un seul onglet courant', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginAdmin(page, g);
  for (const tab of ['', '?onglet=ajuster', '?onglet=historique']) {
    await open(page, `/${g.slug}/admin/tirage${tab}`);
    await expect(page.locator('.drw-tab')).toHaveCount(3);
    await expect(page.locator('.drw-tab[aria-current="page"]')).toHaveCount(1);
    await expectCleanAdminPage(page);
  }
});
