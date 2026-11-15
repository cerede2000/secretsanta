import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { INVITE_PATH, requireInvite } from './invite';

const SELECT = 'footer .lang-selector-select';
const APPLY = 'footer .lang-selector-apply';

// F11c.AC14 : le selecteur de langue est visible des le formulaire, avant
// toute identification, et la langue choisie suit l'inscription (elle est
// enregistree comme locale du participant, verifie cote Go).
test.beforeEach(requireInvite);

test('le selecteur de langue est visible sur le formulaire, sans defilement horizontal', async ({ page }) => {
  await page.goto(INVITE_PATH);
  await page.locator('footer').scrollIntoViewIfNeeded();
  await expect(page.locator(SELECT)).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator(`${SELECT}, ${APPLY}`));
});

test('la langue choisie traduit le formulaire et la saisie du code', async ({ page, context }) => {
  await page.goto(INVITE_PATH);
  await page.selectOption(SELECT, 'en');
  await Promise.all([page.waitForURL(/rejoindre/), page.locator(APPLY).click()]);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('h1')).toHaveText('Join the Secret Santa');
  expect((await context.cookies()).find((c) => c.name === 'lang')?.value).toBe('en');
  const unique = Date.now().toString(36);
  await page.fill('#reg-first', 'Camille');
  await page.fill('#reg-name', `Lang ${unique}`);
  await page.fill('#reg-email', `lang.${unique}@exemple.test`);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('h1')).toHaveText('Check your mail');
  await expectNoHorizontalScroll(page);
});
