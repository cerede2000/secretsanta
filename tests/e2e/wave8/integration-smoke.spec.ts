import { expect, test, type Page } from '@playwright/test';
import { expectNoHorizontalScroll } from '../_shared';
import { seed } from '../_shared/seed';

// Smoke du cablage de la vague 8 sur l'application reelle : pages
// d'administration (menu, tableau de bord, controle, journal, roles), page du
// participant renvoyee vers la connexion, reponse neutre du lien d'inscription.
// Le mot de passe d'instance vient de l'amorcage (_shared/seed.ts).
const instancePassword = (): string => seed().instancePassword;

function uniqueSlug(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

async function watchCSP(page: Page): Promise<string[]> {
  const problems: string[] = [];
  page.on('console', (m) => {
    if (/content security policy|refused to/i.test(m.text())) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));
  await page.addInitScript(() => {
    (window as any).__csp = [];
    document.addEventListener('securitypolicyviolation', (e) =>
      (window as any).__csp.push(`${e.violatedDirective} ${e.blockedURI}`),
    );
  });
  return problems;
}

test('administration : pages montees, navigation resolue, CSP stricte, mobile sans debordement', async ({ page }) => {
  const problems = await watchCSP(page);
  const slug = uniqueSlug('w8');

  await page.goto('/creer');
  await page.fill('#g-password', instancePassword());
  await page.fill('#g-name', 'Vague huit');
  await page.fill('#g-slug', slug);
  await page.click('.groups-submit');
  const secret = (await page.locator('#admin-secret').textContent())?.trim() ?? '';
  expect(secret).not.toBe('');

  // Sans session, l'administration renvoie a la connexion ; la connexion reste servie.
  await page.goto(`/${slug}/admin`);
  await expect(page).toHaveURL(new RegExp(`/${slug}/admin/connexion$`));
  await page.fill('#access-secret', secret);
  await page.click('.access-submit');
  await expect(page).toHaveURL(new RegExp(`/${slug}/admin$`));

  const pages: Array<[string, number]> = [
    [`/${slug}/admin`, 200],
    [`/${slug}/admin/plus`, 200],
    [`/${slug}/admin/roles`, 200],
    [`/${slug}/admin/journal`, 200],
    [`/${slug}/admin/inscriptions`, 200],
    [`/${slug}/admin/participants`, 200],
    [`/${slug}/admin/tirage`, 200],
    [`/${slug}/admin/controle`, 409], // tirage pas encore fait : page rendue avec son message
  ];
  for (const [path, status] of pages) {
    const res = await page.goto(path);
    expect(res?.status(), path).toBe(status);
    expect(res?.headers()['content-security-policy'], `${path} : CSP absente`).toBeTruthy();
    await page.waitForLoadState('networkidle');
    expect(await page.locator('[style]').count(), `${path} : style en ligne`).toBe(0);
    await expectNoHorizontalScroll(page);
    // La navigation n'a plus de {slug} litteral et pointe vers ce groupe.
    const hrefs = await page.locator('.admin-nav a').evaluateAll((els) => els.map((e) => e.getAttribute('href') ?? ''));
    expect(hrefs.length, `${path} : entrees de navigation`).toBe(5);
    for (const h of hrefs) expect(h, path).toMatch(new RegExp(`^/${slug}/admin`));
    expect(await page.locator('.admin-nav [aria-current="page"]').count(), `${path} : entree active`).toBe(1);
  }

  // Un clic sur « Plus » mene a la feuille « Plus ».
  await page.goto(`/${slug}/admin`);
  await page.locator(`.admin-nav a[href="/${slug}/admin/plus"]`).click();
  await expect(page).toHaveURL(new RegExp(`/${slug}/admin/plus$`));

  expect(await page.evaluate(() => (window as any).__csp)).toEqual([]);
  expect(problems).toEqual([]);
});

test('participant : /participant renvoie vers la connexion, le lien d\'inscription inconnu donne la reponse neutre', async ({ page }) => {
  const slug = uniqueSlug('w8p');
  await page.goto('/creer');
  await page.fill('#g-password', instancePassword());
  await page.fill('#g-name', 'Vague huit');
  await page.fill('#g-slug', slug);
  await page.click('.groups-submit');
  await expect(page.locator('#admin-secret')).toBeVisible();

  await page.goto(`/${slug}/participant`);
  await expect(page).toHaveURL(new RegExp(`/${slug}/connexion$`));
  await page.goto(`/${slug}/ma-cible`);
  await expect(page).toHaveURL(new RegExp(`/${slug}/connexion$`));

  const neutral = await page.goto(`/${slug}/rejoindre/jeton-inconnu`);
  expect(neutral?.status()).toBe(404);
  const unknown = await page.goto('/groupe-inexistant/rejoindre/jeton-inconnu');
  expect(unknown?.status()).toBe(404);
});
