import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';
import { waitForMailWhere } from '../../mail-sink';

// F11c.AC8 (administration des inscriptions) : la page d'administration des
// inscriptions n'a aucun defilement horizontal, ses zones tactiles font 44 px en
// 360x640 et 390x844, et chaque action fonctionne (interrupteur, lien et QR code,
// renouvellement avec confirmation integree, limites, validation sans code,
// renvoi du code, suppression). Donnees : groupes « reg » (3 inscrits, 3 en
// attente) et « people » (renouvellement) et « roles » (interrupteur, limites) du
// couloir ; ces deux derniers ont un lien actif que aucune autre spec n'utilise, _shared/seed.ts.

test('page : lien, QR code, compteurs, liste en attente, sans debordement ni cible trop petite', async ({ page, context }, testInfo) => {
  const g = lane(testInfo).reg;
  const dialogs = trackDialogs(page);
  await context.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => undefined);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  await expect(page.locator('h1')).toHaveText(/Inscriptions par lien/);
  await expect(page.locator('.regadm-switch')).toHaveAttribute('aria-checked', 'true');
  await expect(page.locator('#regadm-link')).toHaveValue(new RegExp(`${g.invitePath}$`));
  // Le QR code est une vraie image servie par une route de meme origine.
  const qr = page.locator('img.regadm-qrimg');
  await expect(qr).toBeVisible();
  expect(await qr.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0), 'QR code charge').toBe(true);
  await expect(page.locator('.regadm-pend').first()).toBeVisible();
  await expectCleanAdminPage(page, dialogs);

  // Copier : le lien part dans le presse-papiers et un message integre l'annonce.
  await page.locator('[data-regadm-button]').click();
  await expect(page.locator('[data-regadm-status]')).toContainText(/copi/i);
  const clip = await page.evaluate(() => navigator.clipboard.readText()).catch(() => null);
  if (clip !== null) expect(clip).toContain(g.invitePath);
  expect(dialogs()).toBe(0);
});

test('page : le lien d\'invitation actif mene au formulaire public, le lien inconnu reste neutre', async ({ page }, testInfo) => {
  const g = lane(testInfo).reg;
  const ok = await page.request.get(g.invitePath);
  expect(ok.status()).toBe(200);
  const bad = await page.request.get(`/${g.slug}/rejoindre/jeton-inconnu-0000000000`);
  expect(bad.status()).toBeLessThan(500);
  expect(await bad.text()).not.toContain('name="prenom"');
});

test('renouvellement du lien : confirmation integree, l\'ancien lien est invalide, le nouveau marche', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  const before = await page.locator('#regadm-link').inputValue();
  await page.locator('a[href$="confirm=renew"]').click();
  const confirm = page.locator('form[action$="/inscriptions/renouveler"], [role="alertdialog"], [role="group"]').first();
  await expect(confirm).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
  await confirm.locator('button[type="submit"]').click();
  await page.waitForURL(/\/admin\/inscriptions/);
  const after = await page.locator('#regadm-link').inputValue();
  expect(after).not.toBe(before);
  expect(new URL(after).pathname).toMatch(new RegExp(`^/${g.slug}/rejoindre/`));
  const old = await page.request.get(new URL(before).pathname);
  expect(await old.text(), 'l\'ancien lien ne montre plus le formulaire').not.toContain('name="prenom"');
  const fresh = await page.request.get(new URL(after).pathname);
  expect(await fresh.text()).toContain('name="prenom"');
  expect(dialogs()).toBe(0);
});

test('interrupteur et limites : fermer, rouvrir, fixer puis retirer le plafond de places', async ({ page }, testInfo) => {
  const g = lane(testInfo).roles;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  const sw = page.locator('.regadm-switch');
  await expect(sw).toHaveAttribute('aria-checked', 'true');
  await sw.click();
  await expect(page.locator('.regadm-switch')).toHaveAttribute('aria-checked', 'false');
  await expectCleanAdminPage(page, dialogs);
  await page.locator('.regadm-switch').click();
  await expect(page.locator('.regadm-switch')).toHaveAttribute('aria-checked', 'true');

  await page.locator('#regadm-cap').fill('50');
  await page.locator('form[action$="/limites"] button[type="submit"]').click();
  await expect(page.locator('.regadm-stat').nth(2).locator('.regadm-num')).toHaveText('50');
  await expectCleanAdminPage(page, dialogs);
  await page.locator('#regadm-cap').fill('');
  await page.locator('form[action$="/limites"] button[type="submit"]').click();
  await expect(page.locator('.regadm-stat').nth(2).locator('.regadm-num')).toHaveText('–');
});

test('en attente : « Valider sans code » inscrit la personne et envoie son lien', async ({ page }, testInfo) => {
  const g = lane(testInfo).reg;
  const who = g.pending[0];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  const card = page.locator('.regadm-pend', { hasText: who.email });
  await expect(card).toBeVisible();
  await card.locator('form[action$="/valider"] button').click();
  await expect(page.locator('.regadm-pend', { hasText: who.email })).toHaveCount(0);
  const welcome = await waitForMailWhere((m) => m.To.some((t) => t.Address === who.email) && /bienvenue|welcome/i.test(m.Subject));
  expect(welcome.Text).toMatch(new RegExp(`/${g.slug}/acces/`));
  await expectCleanAdminPage(page, dialogs);
});

test('en attente : « Renvoyer le code » repond dans l\'interface (envoi ou delai d\'attente), sans boite native', async ({ page }, testInfo) => {
  const g = lane(testInfo).reg;
  const who = g.pending[1];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  const card = page.locator('.regadm-pend', { hasText: who.email });
  await card.locator('form[action$="/renvoyer"] button').click();
  await page.waitForURL(/\/admin\/inscriptions/);
  // Deux issues legitimes : un nouveau code part, ou le delai d'une minute entre
  // deux envois est annonce par un message integre (role=alert).
  const alert = page.locator('[role="alert"]');
  const status = page.locator('[role="status"]:not([data-regadm-status]):not(:empty)');
  await expect(alert.or(status).first()).toBeVisible();
  await expect(page.locator('.regadm-pend', { hasText: who.email })).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
});

test('en attente : « Supprimer » retire l\'inscription', async ({ page }, testInfo) => {
  const g = lane(testInfo).reg;
  const who = g.pending[2];
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  await page.locator('.regadm-pend', { hasText: who.email }).locator('form[action$="/supprimer"] button').click();
  await expect(page.locator('.regadm-pend', { hasText: who.email })).toHaveCount(0);
  await expectCleanAdminPage(page, dialogs);
  expect(dialogs()).toBe(0);
});
