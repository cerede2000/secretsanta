import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared';
import { seed } from '../_shared/seed';

// Routes attendues (branchees par l'orchestrateur) : GET /creer, POST /creer,
// GET /<slug>. Le mot de passe d'instance vient de l'amorcage (_shared/seed.ts).
const instancePassword = (): string => seed().instancePassword;

function uniqueSlug(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

test('creation de groupe : formulaire mobile sans defilement horizontal ni cible trop petite', async ({ page }) => {
  const res = await page.goto('/creer');
  expect(res?.status()).toBe(200);
  await expect(page.locator('#g-budget')).toHaveValue('');
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('creation de groupe : erreur integree a la page, jamais de boite du navigateur', async ({ page }) => {
  let dialogs = 0;
  page.on('dialog', async (d) => {
    dialogs += 1;
    await d.dismiss();
  });
  await page.goto('/creer');
  await page.fill('#g-password', 'mauvais-mot-de-passe');
  await page.fill('#g-name', 'Famille test');
  await page.fill('#g-slug', uniqueSlug('erreur'));
  await page.click('.groups-submit');
  const alert = page.locator('[role="alert"]');
  await expect(alert).toBeVisible();
  await expect(page.locator('#g-name')).toHaveValue('Famille test');
  await expect(page.locator('#g-password')).toHaveValue('');
  await expectNoHorizontalScroll(page);
  expect(dialogs).toBe(0);
});

test('creation puis accueil : secret affiche une fois, bouton Copier, accueil sans participant', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => undefined);
  const slug = uniqueSlug('noel');
  await page.goto('/creer');
  await page.fill('#g-password', instancePassword());
  await page.fill('#g-name', 'Noel en famille');
  await page.fill('#g-slug', slug);
  await page.click('.groups-submit');

  const secret = page.locator('#admin-secret');
  await expect(secret).toBeVisible();
  const value = (await secret.textContent())?.trim() ?? '';
  expect(value.length).toBeGreaterThan(8);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);

  const status = page.locator('#copy-status');
  await expect(status).toBeHidden();
  await page.click('[data-copy-target="admin-secret"]');
  await expect(status).toBeVisible();
  const clip = await page.evaluate(() => navigator.clipboard.readText()).catch(() => null);
  if (clip !== null) expect(clip).toBe(value);

  // Accueil du groupe : nom et boutons d'entree, jamais le secret.
  const home = await page.goto(`/${slug}`);
  expect(home?.status()).toBe(200);
  await expect(page.locator('h1')).toHaveText('Noel en famille');
  expect(await page.content()).not.toContain(value);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  const enter = page.locator(`a[href="/${slug}/connexion"]`).first();
  const box = await enter.boundingBox();
  expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
});

test('accueil d un groupe inconnu : page neutre 404 sans defilement horizontal', async ({ page }) => {
  const res = await page.goto(`/${uniqueSlug('inconnu')}`);
  expect(res?.status()).toBe(404);
  await expectNoHorizontalScroll(page);
});

test('creation de groupe : adresse reservee, erreur sous le champ avec bordure rouge', async ({ page }) => {
  await page.goto('/creer');
  await page.fill('#g-password', instancePassword());
  await page.fill('#g-name', 'Noel en famille');
  await page.fill('#g-slug', 'admin');
  await page.click('.groups-submit');
  const err = page.locator('#slug-error');
  await expect(err).toBeVisible();
  await expect(err).toHaveAttribute('role', 'alert');
  await expect(err).toContainText('« admin » est réservé');
  await expect(page.locator('#g-slug')).toHaveAttribute('aria-describedby', 'slug-error');
  await expect(page.locator('#g-slug')).toHaveCSS('border-top-color', 'rgb(196, 30, 58)');
  await expect(page.locator('#create-error')).toHaveCount(0);
  await expectNoHorizontalScroll(page);
});
