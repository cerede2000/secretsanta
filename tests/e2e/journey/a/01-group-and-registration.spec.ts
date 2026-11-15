import { expect, test } from '@playwright/test';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';
import { adminLogin, expectCleanMobile, newVisitor, tag } from './kit';

// F12b.AC1, parcours 01 : création du groupe protégée par le mot de passe
// d'instance, activation du lien d'invitation, inscription d'une personne par
// ce lien avec le code lu dans le serveur SMTP factice, puis validation sans code
// d'une seconde inscription par l'administrateur. Tout par les vraies pages.

test('parcours 01 : groupe, lien d\'invitation, inscription avec code, validation par l\'administrateur', async ({ browser }, testInfo) => {
  const t = tag(testInfo);
  const slug = `j${t}`;
  const admin = await newVisitor(browser, testInfo);
  const page = await admin.newPage();

  // 1. Mauvais mot de passe d'instance : refus, aucun groupe créé.
  await page.goto('/creer');
  await expectCleanMobile(page);
  await page.fill('#g-password', 'pas-le-bon-mot-de-passe');
  await page.fill('#g-name', `Parcours ${t}`);
  await page.fill('#g-slug', slug);
  await page.locator('form.groups-form button[type="submit"]').click();
  await expect(page.locator('#create-error')).toBeVisible();
  expect((await page.request.get(`/${slug}`, { failOnStatusCode: false })).status(), 'groupe non créé').toBe(404);

  // 2. Bon mot de passe, budget laissé vide : le secret n'est montré qu'une fois.
  await page.fill('#g-password', process.env.E2E_INSTANCE_PASSWORD!);
  await page.fill('#g-name', `Parcours ${t}`);
  await page.fill('#g-slug', slug);
  await page.locator('form.groups-form button[type="submit"]').click();
  const secret = page.locator('#admin-secret');
  await expect(secret).toBeVisible();
  const adminSecret = (await secret.textContent())!.trim();
  expect(adminSecret.length).toBeGreaterThan(10);
  await expectCleanMobile(page);

  // 3. Connexion d'administration, activation du lien d'invitation.
  await adminLogin(page, { slug, adminSecret });
  await page.goto(`/${slug}/admin/inscriptions`);
  const mode = page.locator('.regadm-switch').first();
  await expect(mode).toHaveAttribute('aria-checked', 'false');
  await mode.click();
  await expect(page.locator('.regadm-switch').first()).toHaveAttribute('aria-checked', 'true');
  const link = new URL(await page.locator('#regadm-link').inputValue()).pathname;
  expect(link).toMatch(new RegExp(`^/${slug}/rejoindre/[A-Za-z0-9_-]+$`));
  await expectCleanMobile(page);

  // 4. Une personne s'inscrit par le lien et saisit le code lu dans le courriel.
  const alice = await newVisitor(browser, testInfo);
  const ap = await alice.newPage();
  const email = `alix.${t}@exemple.test`;
  await ap.goto(link);
  await expectCleanMobile(ap);
  await ap.fill('#reg-first', 'Alix');
  await ap.fill('#reg-name', 'Parcours');
  await ap.fill('#reg-email', email);
  await ap.locator('form.reg-card button[type="submit"]').click();
  await expect(ap.locator('#reg-code')).toBeVisible();
  const mail = await waitForMailWhere((m) => m.To.some((x) => x.Address === email) && /v[ée]rification/i.test(m.Subject));
  const code = extractVerificationCode(mail);
  expect(code, 'code lu dans le serveur SMTP factice').toMatch(/^[A-Za-z0-9]{4,12}$/);
  await ap.fill('#reg-code', code!);
  await ap.locator('form.reg-card button[type="submit"]').click();
  await expect(ap.locator(`a[href^="/${slug}/lettre"][href*="bienvenue=1"]`).first()).toBeVisible();
  await expectCleanMobile(ap);
  await waitForMailWhere((m) => m.To.some((x) => x.Address === email) && /bienvenue/i.test(m.Subject));

  // 5. Une seconde personne s'inscrit mais n'a pas son code : l'administrateur la valide.
  const bob = await newVisitor(browser, testInfo);
  const bp = await bob.newPage();
  const email2 = `bastien.${t}@exemple.test`;
  await bp.goto(link);
  await bp.fill('#reg-first', 'Bastien');
  await bp.fill('#reg-name', 'Parcours');
  await bp.fill('#reg-email', email2);
  await bp.locator('form.reg-card button[type="submit"]').click();
  await expect(bp.locator('#reg-code')).toBeVisible();

  await page.goto(`/${slug}/admin/inscriptions`);
  const pending = page.locator('.regadm-pend', { hasText: 'Bastien' });
  await expect(pending).toBeVisible();
  await expect(page.locator('.regadm-num').first()).toHaveText('1');
  await expectCleanMobile(page);
  await pending.locator('button[type="submit"]').first().click();

  await page.goto(`/${slug}/admin/participants`);
  await expect(page.locator('.ppl-name', { hasText: 'Alix' })).toBeVisible();
  await expect(page.locator('.ppl-name', { hasText: 'Bastien' })).toBeVisible();
  await expectCleanMobile(page);
  await waitForMailWhere((m) => m.To.some((x) => x.Address === email2) && /bienvenue/i.test(m.Subject));
  // Plus aucune inscription en attente.
  await page.goto(`/${slug}/admin/inscriptions`);
  await expect(page.locator('.regadm-pend')).toHaveCount(0);
  await Promise.all([admin.close(), alice.close(), bob.close()]);
});
