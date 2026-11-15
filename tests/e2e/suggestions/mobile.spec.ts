import { expect, test, type Page } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { lane, loginPerson } from '../_shared/seed';

// F8c.AC5 : la page de suggestions (screen-suggestions), sur le groupe d'amorçage
// dont la révélation est ouverte, n'a aucun défilement horizontal et toutes ses
// zones tactiles mesurent au moins 44 px en 360x640 et 390x844. Les sources de
// recherche peuvent être absentes de la recette : la page affiche alors son
// message « aucune source disponible », qui doit lui aussi tenir dans l'écran.

const TARGETS = '.sugg-back, .sugg-chip, .sugg-see, .sugg-refresh-btn, .sugg-apply, .sugg-field input';

/** Adresse de la page de suggestions de la première cible découverte de `page`. */
async function ideasPath(page: Page, slug: string): Promise<string> {
  await page.goto(`/${slug}/boite`);
  const href = await page.locator('a[href*="/lettre-recue/"]').first().getAttribute('href');
  const pair = /\/lettre-recue\/(\d+)/.exec(href ?? '')?.[1];
  if (!pair) throw new Error(`aucune lettre reçue dans /${slug}/boite`);
  return `/${slug}/suggestions/${pair}`;
}

test('screen-suggestions : aucun défilement horizontal, zones tactiles de 44 px', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[0]);
  await page.goto(await ideasPath(page, g.slug));
  await expect(page.locator('h1.sugg-title')).toBeVisible();
  await expect(page.locator('#pmin')).toBeVisible();
  await expect(page.locator('#pmax')).toBeVisible();
  await expect(page.locator('.sugg-privacy')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator(TARGETS));
  expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
});

test('screen-suggestions : le filtre de prix libre reste dans l\'écran, erreur sous le champ', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[0]);
  const path = await ideasPath(page, g.slug);
  await page.goto(path);
  await page.locator('#pmin').fill('abc');
  await page.locator('.sugg-apply').click();
  const error = page.locator('#pmin-err');
  await expect(error).toBeVisible();
  await expect(page.locator('#pmin')).toHaveAttribute('aria-invalid', 'true');
  await expect(page.locator('#pmin')).toHaveAttribute('aria-describedby', 'pmin-err');
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator(TARGETS));

  // Une fourchette valide revient sur la page avec ses valeurs.
  await page.locator('#pmin').fill('10');
  await page.locator('#pmax').fill('30,5');
  await page.locator('.sugg-apply').click();
  await expect(page.locator('#pmin')).toHaveValue('10');
  await expect(page.locator('#pmax')).toHaveValue('30,5');
  await expect(page.locator('#pmin-err')).toHaveCount(0);
  await expectNoHorizontalScroll(page);
});
