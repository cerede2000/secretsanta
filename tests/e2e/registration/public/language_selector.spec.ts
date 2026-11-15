import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { asNewVisitor, lane } from '../../_shared/seed';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';

const SELECT = 'footer .lang-selector-select';
const APPLY = 'footer .lang-selector-apply';

// F11c.AC14 : le selecteur de langue est visible des le formulaire, avant
// toute identification, et la langue choisie suit l'inscription (elle est
// enregistree comme locale du participant, verifie cote Go).
// Donnees : groupe d'amorcage « reg » du couloir (lien d'invitation actif, _shared/seed.ts).
// Chaque test se presente comme un visiteur distinct (limiteur d'inscriptions par adresse IP).
test.beforeEach(async ({ context }) => {
  await asNewVisitor(context);
});

test('le selecteur de langue est visible sur le formulaire, sans defilement horizontal', async ({ page }, testInfo) => {
  const INVITE_PATH = lane(testInfo).reg.invitePath;
  await page.goto(INVITE_PATH);
  await page.locator('footer').scrollIntoViewIfNeeded();
  await expect(page.locator(SELECT)).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator(`${SELECT}, ${APPLY}`));
});

test('la langue choisie traduit le formulaire et la saisie du code', async ({ page, context }, testInfo) => {
  const INVITE_PATH = lane(testInfo).reg.invitePath;
  await page.goto(INVITE_PATH);
  await page.selectOption(SELECT, 'en');
  await Promise.all([page.waitForURL(/rejoindre/), page.locator(APPLY).click()]);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('h1')).toHaveText('Join the Secret Santa');
  expect((await context.cookies()).find((c) => c.name === 'lang')?.value).toBe('en');
  const unique = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
  await page.fill('#reg-first', 'Camille');
  await page.fill('#reg-name', `Lang ${unique}`);
  await page.fill('#reg-email', `lang.${unique}@exemple.test`);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('h1')).toHaveText('Check your mail');
  await expectNoHorizontalScroll(page);

  // La langue choisie est enregistree comme locale du participant : le code, puis
  // le courriel de bienvenue (envoye apres la confirmation) arrivent en anglais.
  const email = `lang.${unique}@exemple.test`;
  const codeMail = await waitForMailWhere((m) => m.To.some((t) => t.Address === email) && /verif/i.test(m.Subject));
  expect(codeMail.Subject).toMatch(/verification code/i);
  const code = extractVerificationCode(codeMail);
  expect(code, 'code lisible dans le courriel').not.toBeNull();
  await page.fill('#reg-code', code!);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('h1')).not.toHaveText('Check your mail');
  const welcome = await waitForMailWhere((m) => m.To.some((t) => t.Address === email) && /welcome|bienvenue/i.test(m.Subject));
  expect(welcome.Subject, 'bienvenue dans la langue enregistree').toMatch(/welcome/i);
  expect(welcome.Text).not.toMatch(/Bonjour|bienvenue/i);
  await expectNoHorizontalScroll(page);
});
