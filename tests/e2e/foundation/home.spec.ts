import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared';
import { HOME_PATH } from './paths';

test('accueil : aucun defilement horizontal et boutons de 44 px minimum', async ({ page }) => {
  const res = await page.goto(HOME_PATH);
  expect(res?.status()).toBe(200);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});
