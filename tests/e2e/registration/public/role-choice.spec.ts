import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { asNewVisitor, lane, loginAdmin } from '../../_shared/seed';

// F11e.AC4 (formulaire) : la section « Un coup de pouce ? (facultatif) » n'a aucun
// defilement horizontal et ses zones tactiles font 44 px en 360x640 et 390x844.
// Donnees : groupe « draft » du couloir (option activee par le test, jamais retiree).
test.beforeEach(async ({ context }) => {
  await asNewVisitor(context);
});

test('choix de role : aucun defilement horizontal, zones tactiles de 44 px', async ({ page }, testInfo) => {
  const g = lane(testInfo).draft;
  await loginAdmin(page, g);
  await page.goto(`/${g.slug}/admin/inscriptions`);
  const sw = page.locator('.regadm-role-switch');
  if ((await sw.getAttribute('aria-checked')) !== 'true') {
    await sw.click();
    await expect(page.locator('.regadm-role-switch')).toHaveAttribute('aria-checked', 'true');
  }
  await page.context().clearCookies();
  await page.goto(g.invitePath);
  const section = page.locator('fieldset[data-reg-roles]');
  await expect(section).toBeVisible();
  await expect(section.locator('legend')).toContainText('Un coup de pouce ?');
  await expect(section.locator('legend')).toContainText('(facultatif)');
  await expect(section.locator('input[type="checkbox"]')).toHaveCount(2);
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(section.locator('label.reg-role'));
  await expectTouchTargets(section.locator('input[type="checkbox"]'), 24);
  await expectNoHorizontalScroll(page);
  expect(await page.locator('[style]').count(), 'style en ligne').toBe(0);
});
