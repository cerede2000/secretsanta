import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { asNewVisitor, lane } from '../../_shared/seed';

// Formulaire d'inscription (screen-inscription) et saisie du code
// (screen-inscription-code) en 360x640 et 390x844 (projets Playwright).
// Donnees : groupe d'amorcage « reg » du couloir (lien d'invitation actif, _shared/seed.ts).
// Chaque test se presente comme un visiteur distinct (limiteur d'inscriptions par adresse IP).
test.beforeEach(async ({ context }) => {
  await asNewVisitor(context);
});

test('formulaire : sans defilement horizontal, cibles de 44 px, fond nocturne de bord a bord', async ({ page }, testInfo) => {
  const INVITE_PATH = lane(testInfo).reg.invitePath;
  await page.goto(INVITE_PATH);
  await expect(page.locator('input[name="prenom"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  await expectTouchTargets(page.locator('input[name="prenom"], input[name="nom"], input[name="email"]'));
  const width = page.viewportSize()!.width;
  const box = await page.locator('.reg-night').boundingBox();
  expect(Math.round(box!.x)).toBe(0);
  expect(Math.round(box!.x + box!.width)).toBe(width);
  expect(await page.locator('.reg-night').evaluate((el) => getComputedStyle(el).borderRadius)).toBe('0px');
  expect(await page.locator('[style]').count(), 'style en ligne').toBe(0);
});

test('formulaire : un champ vide affiche son message SOUS le champ, role=alert', async ({ page }, testInfo) => {
  const INVITE_PATH = lane(testInfo).reg.invitePath;
  await page.goto(INVITE_PATH);
  await page.locator('form.reg-card').filter({ has: page.locator('input[name="prenom"]') }).locator('button[type="submit"]').click();
  const first = page.locator('#reg-first');
  await expect(first).toHaveAttribute('aria-invalid', 'true');
  await expect(first).toHaveAttribute('aria-describedby', 'reg-first-err');
  const msg = page.locator('#reg-first-err');
  await expect(msg).toHaveAttribute('role', 'alert');
  const [fieldBox, msgBox] = [await first.boundingBox(), await msg.boundingBox()];
  expect(msgBox!.y).toBeGreaterThanOrEqual(fieldBox!.y + fieldBox!.height - 1);
  await expectNoHorizontalScroll(page);
});

test('saisie du code : affichee apres l\'envoi, sans defilement horizontal, cibles de 44 px', async ({ page }, testInfo) => {
  const INVITE_PATH = lane(testInfo).reg.invitePath;
  await page.goto(INVITE_PATH);
  const unique = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
  await page.fill('#reg-first', 'Camille');
  await page.fill('#reg-name', `Test ${unique}`);
  await page.fill('#reg-email', `camille.${unique}@exemple.test`);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('#reg-code')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  // Un code faux : message integre a l'interface, jamais une boite du navigateur.
  let dialogs = 0;
  page.on('dialog', async (d) => {
    dialogs++;
    await d.dismiss();
  });
  await page.fill('#reg-code', 'ZZZZZZ');
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('#reg-code-err')).toHaveAttribute('role', 'alert');
  await expect(page.locator('#reg-code')).toHaveAttribute('aria-invalid', 'true');
  expect(dialogs).toBe(0);
});
