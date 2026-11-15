import { expect, test, type Page } from '@playwright/test';
import { expectNoHorizontalScroll } from '../../_shared/mobile';
import { smallTargets, open } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';

// F10.AC20 : les fragments de F3 (formulaire de participant, foyers, exclusions,
// forcages, confirmation de suppression), rendus SEULS dans une page d'essai,
// n'ont aucun defilement horizontal en 360 px et gardent des zones tactiles de
// 44 px. Le fragment est pris tel que le serveur le rend (page reelle, session
// administrateur) puis rejoue seul, sans la mise en page de la page ni la barre
// de navigation, avec les feuilles de style de la page.
const FRAGMENTS: Array<[string, string]> = [
  ['formulaire de participant', 'form.ppl-form:has(input[name="first_name"])'],
  ['foyers', 'form.ppl-form:has(input[name="kind"][value="household"])'],
  ['exclusions', 'form.ppl-form:has(input[name="kind"][value="exclusion"])'],
  ['forcages', 'form.ppl-form:has(input[name="kind"][value="forced"]):has(select#ppl-f-a)'],
  ['retrait d\'un forcage', 'form.ppl-form:has(select#ppl-u-a)'],
  ['confirmation de suppression', 'section.ppl-confirm'],
];

const TEST_PAGE = '/__fragment_harness__';

async function renderAlone(page: Page, source: string[], fragment: string): Promise<void> {
  const html = `<!doctype html><html lang="fr"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
${source.map((h) => `<link rel="stylesheet" href="${h}">`).join('\n')}
</head><body><main id="main" class="site-main">${fragment}</main></body></html>`;
  await page.route(`**${TEST_PAGE}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/html; charset=utf-8',
      headers: { 'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'" },
      body: html,
    }),
  );
  await page.goto(TEST_PAGE);
}

for (const [name, selector] of FRAGMENTS) {
  test(`fragment « ${name} » rendu seul : pas de debordement, cibles de 44 px`, async ({ page }, testInfo) => {
    const g = lane(testInfo).people;
    await loginAdmin(page, g);
    await open(page, `/${g.slug}/admin/participants?supprimer=${g.people[4].id}`);
    const sheets = await page.locator('link[rel="stylesheet"]').evaluateAll((els) => els.map((e) => e.getAttribute('href')!));
    const fragment = await page.locator(selector).first().evaluate((el) => el.outerHTML);
    expect(fragment.length).toBeGreaterThan(50);
    await page.setViewportSize({ width: 360, height: 640 });
    await renderAlone(page, sheets, fragment);
    await expectNoHorizontalScroll(page);
    expect(await smallTargets(page), 'cibles de moins de 44 px').toEqual([]);
    expect(await page.locator('[style]').count(), 'style en ligne').toBe(0);
  });
}
