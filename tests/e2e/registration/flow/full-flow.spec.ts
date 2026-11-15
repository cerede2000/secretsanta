import { expect, test } from '@playwright/test';
import { asNewVisitor, lane, loginAdmin } from '../../_shared/seed';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';

// F11e.AC1 : parcours complet. L'administrateur active le mode, une personne
// s'inscrit, saisit le code recu par courriel, devient participante et arrive
// sur sa lettre. Donnees : groupe « draft » du couloir (le mode y est deja actif :
// le test ne le bascule que s'il est ferme, pour ne pas renouveler le lien).
test('parcours complet : mode actif, inscription, code, lettre', async ({ page, context }, testInfo) => {
  const g = lane(testInfo).draft;
  await loginAdmin(page, g);
  await page.goto(`/${g.slug}/admin/inscriptions`);
  const mode = page.locator('.regadm-switch').first();
  if ((await mode.getAttribute('aria-checked')) !== 'true') {
    await mode.click();
    await expect(page.locator('.regadm-switch').first()).toHaveAttribute('aria-checked', 'true');
  }
  const link = new URL(await page.locator('#regadm-link').inputValue()).pathname;

  await context.clearCookies();
  await asNewVisitor(context);
  const unique = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
  const email = `parcours.${unique}@exemple.test`;
  await page.goto(link);
  await page.fill('#reg-first', 'Camille');
  await page.fill('#reg-name', `Parcours ${unique}`);
  await page.fill('#reg-email', email);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('#reg-code')).toBeVisible();
  const mail = await waitForMailWhere((m) => m.To.some((t) => t.Address === email) && /v[ée]rification/i.test(m.Subject));
  await page.fill('#reg-code', extractVerificationCode(mail)!);
  await page.locator('form.reg-card button[type="submit"]').click();

  const cta = page.locator(`a[href^="/${g.slug}/lettre"][href*="bienvenue=1"]`).first();
  await expect(cta).toBeVisible();
  await cta.click();
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/lettre`));
});
