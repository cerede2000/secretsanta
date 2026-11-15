import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { asNewVisitor, lane } from '../../_shared/seed';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';

// F11c.AC8 (ecran de reussite, screen-inscription-reussie) : apres la saisie du
// vrai code lu dans le courriel, l'ecran de reussite n'a aucun defilement
// horizontal, ses zones tactiles font 44 px, il propose en bouton principal la
// page d'ecriture de la lettre (bienvenue=1) et rappelle le budget du groupe.
// Donnees : groupe « reg » du couloir (budget 25), _shared/seed.ts.
test.beforeEach(async ({ context }) => {
  await asNewVisitor(context);
});

test('reussite : lettre en bouton principal, budget rappele, mobile', async ({ page }, testInfo) => {
  const g = lane(testInfo).reg;
  const unique = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
  const email = `reussite.${unique}@exemple.test`;
  await page.goto(g.invitePath);
  await page.fill('#reg-first', 'Camille');
  await page.fill('#reg-name', `Reussite ${unique}`);
  await page.fill('#reg-email', email);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('#reg-code')).toBeVisible();
  const mail = await waitForMailWhere((m) => m.To.some((t) => t.Address === email) && /v[ée]rification/i.test(m.Subject));
  await page.fill('#reg-code', extractVerificationCode(mail)!);
  await page.locator('form.reg-card button[type="submit"]').click();

  const cta = page.locator(`a[href^="/${g.slug}/lettre"][href*="bienvenue=1"]`).first();
  await expect(cta).toBeVisible();
  await expect(page.locator('main')).toContainText('25');
  expect(await page.locator('[style]').count(), 'style en ligne').toBe(0);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  await expectTouchTargets(cta);
  // Le lien de la lettre ouvre bien la page d'ecriture, personne connectee.
  await cta.click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/lettre`));
  await expectNoHorizontalScroll(page);
});
