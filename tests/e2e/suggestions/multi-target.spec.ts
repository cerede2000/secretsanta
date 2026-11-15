import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { lane, loginPerson } from '../_shared/seed';

// F8c.3 : un tireur à deux cibles (people[1], offre_plus) choisit le cadeau sur
// la page de suggestions ; le sélecteur tient dans l'écran mobile, la cible
// courante est marquée et la seconde, non découverte, mène à la découverte.

test('screen-suggestions : sélecteur « Cadeau 1 / Cadeau 2 » pour deux cibles', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[1]);
  await page.goto(`/${g.slug}/boite`);
  const href = await page.locator('a[href*="/lettre-recue/"]').first().getAttribute('href');
  const pair = /\/lettre-recue\/(\d+)/.exec(href ?? '')?.[1];
  expect(pair, 'lettre reçue de la première cible').toBeTruthy();
  await page.goto(`/${g.slug}/suggestions/${pair}`);

  const chips = page.locator('.sugg-chip');
  await expect(chips).toHaveCount(2);
  await expect(chips.first()).toHaveAttribute('aria-current', 'true');
  await expect(chips.nth(1)).not.toHaveAttribute('aria-current', 'true');
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.sugg-chip, .sugg-apply, .sugg-field input'));
  expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);

  // La seconde cible n'est pas découverte : le lien mène à la découverte.
  await chips.nth(1).click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/decouverte`));
});

test('screen-suggestions : une seule cible, pas de sélecteur', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[0]);
  await page.goto(`/${g.slug}/boite`);
  const href = await page.locator('a[href*="/lettre-recue/"]').first().getAttribute('href');
  const pair = /\/lettre-recue\/(\d+)/.exec(href ?? '')?.[1];
  await page.goto(`/${g.slug}/suggestions/${pair}`);
  await expect(page.locator('.sugg-chip')).toHaveCount(0);
});
