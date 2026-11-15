import { expect, test, type Page } from '@playwright/test';
import { expectCleanAdminPage, trackDialogs } from '../../_shared/admin';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared/mobile';
import { loginAdmin, loginPerson, type Person } from '../../_shared/seed';
import { createGroupWithPeople, uniqueTag, writeLetter } from './support';

// Parcours 07 (F12b.AC13) : rôles dans le tirage et deux cibles.
//  1. l'administrateur règle les rôles par le panneau « Rôles dans le tirage » ;
//  2. le tirage donne deux cibles au tireur offre_plus (une personne ne fait que recevoir) ;
//  3. la révélation des deux cibles se joue en séquence, avec deux styles différents ;
//  4. les deux lettres reçues se lisent ;
//  5. la personne reception_seule voit son accueil ;
//  6. un changement de rôle après le tirage passe par l'aperçu en comptes, sans nouveau tirage.
// Groupe neuf, créé par l'application réelle : aucune autre spec ne le partage.

/** Le navigateur change de personne : même page, cookies de session effacés. */
async function logout(page: Page): Promise<void> {
  await page.context().clearCookies();
}

test('parcours 07 : rôles, deux cibles en séquence, lettres, accueil réception seule, changement après tirage', async ({
  page,
}, testInfo) => {
  test.setTimeout(240_000);
  const dialogs = trackDialogs(page);
  const tag = uniqueTag(testInfo.project.name);
  const { group: g } = await createGroupWithPeople(tag, 'roles', 6);
  const [giver, receiver, ...others] = g.people;

  // Les lettres sont écrites avant le tirage (par les pages réelles).
  for (const p of g.people) await writeLetter(g, p, `Bonjour, ici ${p.firstName}.`);

  // 1. Panneau « Rôles dans le tirage » (section Tirage de l'administration).
  await loginAdmin(page, g);
  await page.goto(`/${g.slug}/admin/tirage`);
  await page.locator(`a[href="/${g.slug}/admin/roles"]`).last().click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/admin/roles`));
  await expect(page.locator('.roles-title')).toBeVisible();
  const setRole = async (who: Person, role: string) => {
    const row = page.locator('.roles-row', { hasText: who.firstName });
    await row.locator('select.roles-select').selectOption(role);
    await expect(page.locator('[role="status"]')).toBeVisible();
    await expect(page.locator('.roles-row', { hasText: who.firstName }).locator('select.roles-select')).toHaveValue(role);
  };
  await setRole(giver, 'offre_plus');
  await setRole(receiver, 'reception_seule');
  // Le récapitulatif ne donne que des comptes, jamais un nom.
  const counts = await page.locator('.roles-counts dd').allTextContents();
  expect(counts.every((c) => /^\d+$/.test(c.trim()))).toBe(true);
  await expect(page.locator('.roles-card').first().locator('.chip-ok')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);

  // 2. Tirage lancé depuis la section Tirage.
  await page.goto(`/${g.slug}/admin/tirage?confirmer=lancer`);
  await page.locator('form[action$="/admin/tirage/lancer"] button[type="submit"]').click();
  await expect(page.locator('form[action$="/admin/tirage/lancer"]')).toHaveCount(0);
  const open = page.locator('a[href$="confirmer=ouvrir"]');
  if (await open.count()) {
    await open.click();
    await page.locator('form[action$="/admin/tirage/ouvrir"] button[type="submit"]').click();
  }
  await expectCleanAdminPage(page, dialogs);

  // 3. Révélation en séquence des deux cibles du tireur offre_plus, graine de test imposée
  //    dans la page (le gabarit réel ne la fournit pas : on l'ajoute à la réponse).
  await logout(page);
  await loginPerson(page, giver);
  await page.route(`**/${g.slug}/decouverte`, async (route) => {
    const res = await route.fetch();
    const html = (await res.text()).replace('data-reveal ', 'data-reveal data-seed="7" ');
    await route.fulfill({ response: res, body: html });
  });
  await page.goto(`/${g.slug}/decouverte`);
  const root = page.locator('[data-reveal]');
  await expect(root).toHaveAttribute('data-seed', '7');
  const stage = page.locator('[data-role="stage"] button.reveal-open');
  const names: string[] = [];
  const styles: string[] = [];
  for (let i = 0; i < 2; i++) {
    await expect(root).toHaveAttribute('data-phase', 'ready');
    styles.push((await root.getAttribute('data-style')) ?? '');
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    await stage.click();
    await expect(root).toHaveAttribute('data-phase', 'revealed');
    names.push((await page.locator('[data-role="name"]').textContent())?.trim() ?? '');
    if (i === 0) await page.locator('[data-role="next"]').click();
  }
  expect(styles.every((s) => ['gift', 'envelope', 'scratch'].includes(s))).toBe(true);
  expect(styles[1], 'deux styles différents en séquence').not.toBe(styles[0]);
  expect(new Set(names).size, 'deux cibles distinctes').toBe(2);
  for (const n of names) {
    expect(n).not.toBe('');
    expect(n).not.toContain(giver.firstName);
  }
  expect(dialogs()).toBe(0);
  await page.unroute(`**/${g.slug}/decouverte`);

  // 4. Les deux lettres reçues se lisent (boîte à lettres, deux entrées).
  await page.goto(`/${g.slug}/boite`);
  const hrefs = await page.locator('a[href*="/lettre-recue/"]').evaluateAll((els) => els.map((e) => e.getAttribute('href') ?? ''));
  expect(hrefs).toHaveLength(2);
  const read: string[] = [];
  for (const href of hrefs) {
    await page.goto(href);
    await expect(page.locator('.letter-tab')).toHaveCount(2);
    const signed = (await page.locator('.letter-signature').textContent())?.trim() ?? '';
    read.push(signed);
    await expect(page.locator('.letter-intro')).toContainText(`ici ${signed.split(' ')[0]}`);
    await expectNoHorizontalScroll(page);
  }
  expect(new Set(read).size).toBe(2);
  expect(read.map((r) => r.split(' ')[0]).sort()).toEqual(names.map((n) => n.split(' ')[0]).sort());

  // 5. La personne reception_seule voit son accueil, sans compte à rebours ni cible à ouvrir.
  await logout(page);
  await loginPerson(page, receiver);
  await page.goto(`/${g.slug}/participant`);
  await expect(page.locator('.draw-reception')).toBeVisible();
  await expect(page.locator('[data-countdown]')).toHaveCount(0);
  await expect(page.locator(`a[href="/${g.slug}/decouverte"]`)).toHaveCount(0);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);

  // 6. Changement de rôle après le tirage : aperçu en comptes, aucun nouveau tirage.
  await logout(page);
  await loginAdmin(page, g);
  const changed = others[0];
  await page.goto(`/${g.slug}/admin/roles`);
  const row = page.locator('.roles-row', { hasText: changed.firstName });
  await row.locator('select.roles-select').selectOption('reception_seule');
  const preview = page.locator('.roles-confirm');
  await expect(preview).toBeVisible();
  const items = await preview.locator('.roles-confirm-counts li').allTextContents();
  expect(items).toHaveLength(3);
  for (const it of items) expect(it).toMatch(/\d/);
  const previewText = await preview.innerText();
  for (const p of g.people) expect(previewText, 'aperçu en comptes, jamais un nom').not.toContain(p.firstName);
  await expectCleanAdminPage(page, dialogs);
  // On garde le tirage tel quel : le changement ne vaut que pour la suite.
  await preview.locator('button[value="future"]').click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/admin/roles\\?`));
  await expect(page.locator('.roles-alert-ok[role="status"]')).toBeVisible();
  await expect(page.locator('.roles-row', { hasText: changed.firstName }).locator('select.roles-select')).toHaveValue('reception_seule');

  // Aucun nouveau tirage : les deux cibles du tireur sont inchangées.
  await logout(page);
  await loginPerson(page, giver);
  await page.goto(`/${g.slug}/boite`);
  const again = await page.locator('a[href*="/lettre-recue/"]').evaluateAll((els) => els.map((e) => e.getAttribute('href') ?? ''));
  expect(again).toEqual(hrefs);
  expect(dialogs()).toBe(0);
});
