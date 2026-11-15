import { expect, test } from '@playwright/test';
import { asNewVisitor, lane, loginAdmin } from '../../_shared/seed';

// F11e.AC5 : aucun choix coche au chargement ; en cocher un decoche l'autre ;
// toucher le choix deja coche l'annule (retour au role normal).
// Donnees : groupe « draft » du couloir (option activee par le test, jamais retiree).
test.beforeEach(async ({ context }) => {
  await asNewVisitor(context);
});

test('choix de role : aucun pre-coche, exclusifs, annulables', async ({ page }, testInfo) => {
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
  const recv = page.locator('#reg-role-recv');
  const extra = page.locator('#reg-role-extra');
  await expect(recv).not.toBeChecked();
  await expect(extra).not.toBeChecked();
  await expect(page.getByText('Je participe normalement')).toHaveCount(0);

  await page.locator('label[for="reg-role-recv"]').click();
  await expect(recv).toBeChecked();
  await expect(extra).not.toBeChecked();
  await page.locator('label[for="reg-role-extra"]').click();
  await expect(extra).toBeChecked();
  await expect(recv).not.toBeChecked();
  // Toucher le choix deja coche l'annule : retour au role normal.
  await page.locator('label[for="reg-role-extra"]').click();
  await expect(extra).not.toBeChecked();
  await expect(recv).not.toBeChecked();
});
