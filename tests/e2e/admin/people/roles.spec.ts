import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';
import type { Page } from '@playwright/test';

// Hors du panneau « Roles dans le tirage », le selecteur garde son bouton
// « Changer » (pas de script d'envoi automatique) : on choisit puis on valide.
async function change(page: Page, form: string, role: string): Promise<void> {
  await page.locator(`${form} select.roles-select`).selectOption(role);
  await Promise.all([
    page.waitForResponse((r) => r.request().method() === 'POST' && r.url().includes('/admin/roles')),
    page.locator(`${form} .roles-apply`).click(),
  ]);
}

// F10.AC29 (selecteur de role) : le selecteur de role de la fiche et celui du
// menu « … » d'une ligne n'ont aucun defilement horizontal, gardent des cibles
// de 44 px, proposent les quatre roles exclusifs et enregistrent le choix ; la
// valeur est relue sur le serveur. Donnees : groupe « people » du couloir (la
// spec remet « Normal » a la fin), _shared/seed.ts.
const ROLES = ['normal', 'reception_seule', 'offre_plus', 'un_seul'];

test('fiche : selecteur a quatre roles, cibles de 44 px, enregistre puis releve', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const p = g.people[0];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants/${p.id}`);
  const select = page.locator('form[data-roles-row] select.roles-select');
  await expect(select).toBeVisible();
  expect(await select.locator('option').evaluateAll((o) => o.map((x) => (x as HTMLOptionElement).value))).toEqual(ROLES);
  await expect(select).toHaveValue('normal');
  const box = (await select.boundingBox())!;
  expect(box.height).toBeGreaterThanOrEqual(43.5);
  await expectCleanAdminPage(page, dialogs);

  await change(page, 'form[data-roles-row]', 'un_seul');
  // Relu apres rechargement : le serveur a bien retenu le role.
  await open(page, `/${g.slug}/admin/participants/${p.id}`);
  await expect(page.locator('form[data-roles-row] select.roles-select')).toHaveValue('un_seul');
  await expectCleanAdminPage(page, dialogs);

  await change(page, 'form[data-roles-row]', 'normal');
  await open(page, `/${g.slug}/admin/participants/${p.id}`);
  await expect(page.locator('form[data-roles-row] select.roles-select')).toHaveValue('normal');
});

test('liste : le selecteur du menu « … » d\'une ligne s\'envoie au changement, badge et filtre suivent', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const p = g.people[1];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/participants`);
  const row = page.locator(`#p-${p.id}`);
  await row.locator('summary.ppl-more').click();
  const select = row.locator('select.roles-select');
  await expect(select).toBeVisible();
  expect((await select.boundingBox())!.height).toBeGreaterThanOrEqual(43.5);
  await expectCleanAdminPage(page, dialogs);

  await change(page, `#p-${p.id}`, 'reception_seule');
  await expectCleanAdminPage(page, dialogs);

  // Le filtre par role retrouve la personne.
  await open(page, `/${g.slug}/admin/participants?role=reception_seule`);
  await expect(page.locator('.ppl-row')).toHaveCount(1);
  await expect(page.locator('.ppl-row .ppl-name')).toHaveText(p.name);
  await expectCleanAdminPage(page, dialogs);

  // Remise a l'etat initial.
  await page.locator(`#p-${p.id} summary.ppl-more`).click();
  await change(page, `#p-${p.id}`, 'normal');
  await open(page, `/${g.slug}/admin/participants?role=normal`);
  await expect(page.locator('.ppl-row')).toHaveCount(g.people.length);
});
