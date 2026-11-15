import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';
import { lane, loginPerson, person } from '../_shared/seed';

// F4d.AC5 et AC9 : la preference « un seul cadeau » avant le tirage. Donnees :
// groupe d'amorcage « draft » du couloir (tirage pas encore fait), personne au
// role normal (_shared/seed.ts).
test('preference « un seul cadeau » : decochee par defaut, bascule, mobile', async ({ page }, testInfo) => {
  const g = lane(testInfo).draft;
  await loginPerson(page, person(g, 'normal'));
  await page.goto(`/${g.slug}/participant`);
  const sw = page.locator('.draw-pref [role="switch"]');
  await expect(sw).toBeVisible();
  await expect(sw).toHaveAttribute('aria-checked', 'false');
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  const box = await sw.boundingBox();
  expect(box!.height).toBeGreaterThanOrEqual(43.5);
  expect(box!.width).toBeGreaterThanOrEqual(43.5);

  await sw.click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/participant$`));
  await expect(page.locator('.draw-pref [role="switch"]')).toHaveAttribute('aria-checked', 'true');
  await expectNoHorizontalScroll(page);

  // Remise a l'etat initial.
  await page.locator('.draw-pref [role="switch"]').click();
  await expect(page.locator('.draw-pref [role="switch"]')).toHaveAttribute('aria-checked', 'false');
});

test('preference : jamais proposee a une personne qui ne fait que recevoir', async ({ page }, testInfo) => {
  const g = lane(testInfo).draft;
  await loginPerson(page, person(g, 'reception_seule'));
  await page.goto(`/${g.slug}/participant`);
  await expect(page.locator('.draw-pref')).toHaveCount(0);
});
