import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { lane, loginPerson } from '../_shared/seed';

// F8c.AC9 : la page de suggestions d'un tireur à deux cadeaux (groupe
// « revealed », people[1], offre_plus) propose le sélecteur de cadeau ; aucun
// défilement horizontal et zones tactiles d'au moins 44 px en 360x640 et
// 390x844 (projets Playwright).

test('suggestions à deux cadeaux : sélecteur sans débordement, cibles de 44 px', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[1]);
  await page.goto(`/${g.slug}/boite`);
  const href = await page.locator('a[href*="/lettre-recue/"]').first().getAttribute('href');
  const pair = /\/lettre-recue\/(\d+)/.exec(href ?? '')?.[1];
  expect(pair, 'lettre reçue de la première cible').toBeTruthy();
  const res = await page.goto(`/${g.slug}/suggestions/${pair}`);
  expect(res!.status()).toBe(200);

  const chips = page.locator('.sugg-gifts .sugg-chip');
  await expect(chips).toHaveCount(2);
  await expect(chips.first()).toHaveAttribute('aria-current', 'true');
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.sugg-gifts .sugg-chip'));
  await expectTouchTargets(page.locator('.sugg-chip, .sugg-apply, .sugg-field input'));
});
