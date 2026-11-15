import { expect, test } from '@playwright/test';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';
import { adminLogin, createGroupHttp, expectCleanMobile, memberLogin, newVisitor, registerMembers, tag } from './kit';

// F12b.AC2, parcours 02 : lettre au Père Noël avec des idées, saisie des foyers et
// des exclusions par l'administrateur, budget laissé vide puis défini (la page
// de réussite d'une inscription ne parle de budget que lorsqu'il existe).

test('parcours 02 : lettre et idées, foyers et exclusions, budget vide puis défini', async ({ browser }, testInfo) => {
  const t = tag(testInfo);
  const f = await createGroupHttp(t); // budget vide
  await registerMembers(f, 4, t);
  const [alix, bastien, cleo, dorian] = f.members;

  // --- Lettre : message, deux idées, une idée supprimée, rechargement ---
  const personCtx = await newVisitor(browser, testInfo);
  const p = await personCtx.newPage();
  await memberLogin(p, alix);
  await p.goto(`/${f.slug}/lettre?bienvenue=1`);
  await expect(p.locator('.letter-welcome')).toBeVisible();
  await expect(p.locator('.letter-empty')).toBeVisible();
  await expectCleanMobile(p, '.letter-back, .letter-send-btn, .letter-add, .letter-input, .letter-ta');
  await p.locator('#intro').fill('Cher Père Noël, cette année je voudrais des choses chaudes et un peu de lecture.');
  await p.locator('.letter-send-btn').click();
  await p.waitForURL(`**/${f.slug}/lettre`);
  await expect(p.locator('.letter-saved')).toContainText('Enregistré');
  for (const [title, url, text] of [
    ['Écharpe en laine', 'https://exemple.test/echarpe', 'Plutôt grise.'],
    ['Roman d\'aventure', '', ''],
    ['Idée à retirer', '', 'Finalement non.'],
  ]) {
    await p.locator('#idea-title').fill(title);
    await p.locator('#idea-url').fill(url);
    await p.locator('#idea-text').fill(text);
    await p.locator('.letter-add').click();
    await p.waitForURL(`**/${f.slug}/lettre`);
  }
  await expect(p.locator('.letter-idea')).toHaveCount(3);
  await p.locator('.letter-idea', { hasText: 'Idée à retirer' }).locator('.letter-del').click();
  await p.waitForURL(`**/${f.slug}/lettre`);
  await expect(p.locator('.letter-idea')).toHaveCount(2);
  await p.reload();
  await expect(p.locator('#intro')).toHaveValue(/choses chaudes/);
  await expect(p.locator('.letter-idea', { hasText: 'Écharpe en laine' })).toBeVisible();
  await expect(p.locator('.letter-idea', { hasText: 'Roman d\'aventure' })).toBeVisible();
  await expectCleanMobile(p, '.letter-back, .letter-del, .letter-send-btn, .letter-add, .letter-input, .letter-ta');

  // --- Foyers et exclusions par l'administrateur ---
  const adminCtx = await newVisitor(browser, testInfo);
  const a = await adminCtx.newPage();
  await adminLogin(a, f);
  await a.goto(`/${f.slug}/admin/participants`);
  const hh = a.locator('form.ppl-form', { has: a.locator('#ppl-hh-name') });
  await hh.locator('#ppl-hh-name').fill('Foyer Alix et Bastien');
  await hh.locator('label.ppl-check', { hasText: alix.firstName }).locator('input').check();
  await hh.locator('label.ppl-check', { hasText: bastien.firstName }).locator('input').check();
  await hh.locator('button[type="submit"]').click();
  await expect(a.locator('.ppl-hh', { hasText: 'Foyer Alix et Bastien' })).toBeVisible();
  await expect(a.locator('.ppl-hh li', { hasText: alix.firstName })).toBeVisible();
  await expect(a.locator('.ppl-hh li', { hasText: bastien.firstName })).toBeVisible();

  await expect(a.locator('.ppl-rules li')).toHaveCount(0);
  const ex = a.locator('form.ppl-form', { has: a.locator('#ppl-x-a') });
  await ex.locator('#ppl-x-a').selectOption({ value: String(cleo.id) });
  await ex.locator('#ppl-x-b').selectOption({ value: String(dorian.id) });
  await ex.locator('button[type="submit"]').click();
  const rule = a.locator('.ppl-rules li');
  await expect(rule).toHaveCount(1);
  await expect(rule).toContainText(cleo.firstName);
  await expect(rule).toContainText(dorian.firstName);
  await expectCleanMobile(a);

  // Retrait de l'exclusion, puis nouvelle saisie : la règle revient.
  await rule.locator('button[type="submit"]').click();
  await expect(a.locator('.ppl-rules li')).toHaveCount(0);
  const ex2 = a.locator('form.ppl-form', { has: a.locator('#ppl-x-a') });
  await ex2.locator('#ppl-x-a').selectOption({ value: String(cleo.id) });
  await ex2.locator('#ppl-x-b').selectOption({ value: String(dorian.id) });
  await ex2.locator('button[type="submit"]').click();
  await expect(a.locator('.ppl-rules li')).toHaveCount(1);

  // --- Budget : vide à la création, puis défini ---
  await a.goto(`/${f.slug}/admin/reglages`);
  await expect(a.locator('#stg-budget')).toHaveValue('');
  const first = await registerAndReadSuccess(browser, testInfo, f.invitePath, `Gina.${t}`, `gina.${t}@exemple.test`);
  expect(first, 'sans budget, la page de réussite n\'en parle pas').not.toMatch(/Budget/);

  await a.goto(`/${f.slug}/admin/reglages`);
  await a.locator('#stg-budget').fill('30');
  await a.locator('form.stg-form', { has: a.locator('#stg-budget') }).locator('button[type="submit"]').click();
  await a.goto(`/${f.slug}/admin/reglages`);
  await expect(a.locator('#stg-budget')).toHaveValue(/^30([.,]0+)?$/);
  await expectCleanMobile(a);
  const second = await registerAndReadSuccess(browser, testInfo, f.invitePath, `Hugo.${t}`, `hugo.${t}@exemple.test`);
  expect(second, 'avec budget, la page de réussite l\'indique').toMatch(/Budget : 30/);
  await Promise.all([personCtx.close(), adminCtx.close()]);
});

/** Inscrit une personne par le lien public et renvoie le texte de la page de réussite. */
async function registerAndReadSuccess(
  browser: import('@playwright/test').Browser,
  testInfo: import('@playwright/test').TestInfo,
  link: string,
  first: string,
  email: string,
): Promise<string> {
  const ctx = await newVisitor(browser, testInfo);
  const page = await ctx.newPage();
  await page.goto(link);
  await page.fill('#reg-first', first.split('.')[0]);
  await page.fill('#reg-name', 'Parcours');
  await page.fill('#reg-email', email);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('#reg-code')).toBeVisible();
  const mail = await waitForMailWhere((m) => m.To.some((x) => x.Address === email) && /v[ée]rification/i.test(m.Subject));
  await page.fill('#reg-code', extractVerificationCode(mail)!);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('.reg-cta')).toBeVisible();
  await expectCleanMobile(page);
  const text = await page.locator('.reg-card-full').first().innerText();
  await ctx.close();
  return text;
}
