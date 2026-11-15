import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';

// F10.AC19 : les pages de participants (liste, fiche, modification, confirmation
// de suppression) n'ont aucun defilement horizontal et leurs zones tactiles
// mesurent au moins 44 px en 360x640 et 390x844. Donnees : groupe d'amorcage
// « people » du couloir (cinq personnes, aucun tirage), _shared/seed.ts.

test('liste : recherche, filtres, menu « … » par ligne, sans debordement ni cible trop petite', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants`);
  await expect(page.locator('.ppl-row')).toHaveCount(g.people.length);
  await expectCleanAdminPage(page, dialogs);

  // Chaque ligne a son menu « … » : il s'ouvre et ses actions font 44 px.
  const first = g.people[0];
  const row = page.locator(`#p-${first.id}`);
  await row.locator('summary.ppl-more').click();
  const items = row.locator('.ppl-menu-list [role="menuitem"]');
  expect(await items.count()).toBeGreaterThanOrEqual(4);
  await expectCleanAdminPage(page, dialogs);
  for (const label of ['Fiche', 'Renvoyer le lien', 'Modifier', 'Supprimer']) {
    await expect(items.filter({ hasText: label })).toBeVisible();
  }

  // La recherche par prenom ne garde que la bonne personne.
  await open(page, `/${g.slug}/admin/participants`);
  await page.locator('#ppl-q').fill(g.people[1].firstName);
  await page.locator('form.ppl-filter button[type="submit"]').click();
  await expect(page.locator('.ppl-row')).toHaveCount(1);
  await expect(page.locator('.ppl-row .ppl-name')).toHaveText(g.people[1].name);
  await expect(page.locator('.ppl-note[role="status"]')).toContainText('1');
  await expectCleanAdminPage(page, dialogs);

  // Un filtre sans resultat ne casse pas la page.
  await page.locator('#ppl-q').fill('zzz-introuvable');
  await page.locator('form.ppl-filter button[type="submit"]').click();
  await expect(page.locator('.ppl-row')).toHaveCount(0);
  await expectCleanAdminPage(page, dialogs);
});

test('liste : ajout d\'une personne par le formulaire, erreur integree sous le champ', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants`);
  // Nom vide : le navigateur ne valide pas (required) ? On envoie sans prenom pour obtenir l'erreur du serveur.
  await page.locator('#ppl-first').evaluate((el: HTMLInputElement) => el.removeAttribute('required'));
  await page.locator('#ppl-name').fill('Sans-prenom');
  await page.locator('form.ppl-form button[type="submit"]').first().click();
  await expect(page.locator('[role="alert"]').first()).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
});

test('fiche : statut d\'acces, role, actions ; jamais de cible', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const p = g.people[2];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants/${p.id}`);
  await expect(page.locator('h1, .ppl-title').first()).toContainText(p.name);
  await expect(page.locator('main')).toContainText(p.email);
  await expectCleanAdminPage(page, dialogs);
  const text = await page.locator('main').innerText();
  for (const other of g.people.filter((x) => x !== p)) {
    expect(text, `la fiche ne nomme pas ${other.name}`).not.toContain(other.name);
  }
  expect(text).toMatch(/Cette fiche ne montre jamais la cible/);
});

test('modification : formulaire prerempli', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const p = g.people[3];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants/${p.id}/modifier`);
  await expect(page.locator('input[name="first_name"]')).toHaveValue(p.firstName);
  await expect(page.locator('input[name="email"]')).toHaveValue(p.email);
  await expectCleanAdminPage(page, dialogs);
});

test('suppression : confirmation integree a la page, jamais une boite du navigateur', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const p = g.people[4];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants?supprimer=${p.id}`);
  const confirm = page.locator('.ppl-confirm[role="alertdialog"]');
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText(p.name);
  await expectCleanAdminPage(page, dialogs);
  // Annuler ramene a la liste sans rien retirer.
  await confirm.locator('a.btn-ghost').click();
  await expect(page.locator('.ppl-confirm')).toHaveCount(0);
  await expect(page.locator('.ppl-row')).toHaveCount(g.people.length);
  expect(dialogs()).toBe(0);
});
