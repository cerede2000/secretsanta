import { expect, test } from '@playwright/test';
import { expectCleanMobile, newVisitor, tag } from './kit';

// Parcours 00 : de l'accueil à un groupe créé, sans jamais taper une adresse.
// Le bouton « Commencer » de `/` doit mener à `/creer` (correctif b95f9af, qui n'avait
// pas de test de parcours), puis le formulaire crée un groupe avec le mot de passe
// d'instance de l'application de test et l'on arrive sur la page du groupe.

test('parcours 00 : accueil, « Commencer », création de groupe, page du groupe', async ({ browser }, testInfo) => {
  const t = tag(testInfo);
  const slug = `h${t}`;
  const visitor = await newVisitor(browser, testInfo);
  const page = await visitor.newPage();

  // 1. Accueil : le bouton « Commencer » est là, lisible au doigt, et mène à /creer.
  await page.goto('/');
  await expectCleanMobile(page, 'main a.button');
  const start = page.getByRole('link', { name: 'Commencer' });
  await expect(start).toBeVisible();
  await start.click();
  await expect(page).toHaveURL(/\/creer$/);
  await expect(page.locator('form.groups-form')).toBeVisible();
  await expect(page.locator('#g-password')).toBeVisible();
  await expect(page.locator('#g-name')).toBeVisible();
  await expectCleanMobile(page);

  // 2. Création avec le mot de passe d'instance.
  await page.fill('#g-password', process.env.E2E_INSTANCE_PASSWORD!);
  await page.fill('#g-name', `Accueil ${t}`);
  await page.fill('#g-slug', slug);
  await page.locator('form.groups-form button[type="submit"]').click();
  await expect(page.locator('#admin-secret')).toBeVisible();
  await expect(page.locator('.groups-link')).toContainText(`/${slug}`);
  await expectCleanMobile(page);

  // 3. Arrivée sur la page du groupe : le lien d'administration affiché mène à l'espace du groupe.
  await page.getByRole('link', { name: /administr|organis|espace/i }).first().click();
  await expect(page).toHaveURL(new RegExp(`/${slug}/admin`));
  await expectCleanMobile(page);
  const group = await page.request.get(`/${slug}`, { failOnStatusCode: false });
  expect(group.status(), 'la page du groupe existe').toBeLessThan(400);
  await visitor.close();
});
