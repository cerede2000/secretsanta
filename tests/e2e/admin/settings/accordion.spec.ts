import { expect, test, type Page } from '@playwright/test';
import { expectNoHorizontalScroll } from '../../_shared/mobile';
import { lane, loginAdmin } from '../../_shared/seed';

// F10c.AC4 : la page des réglages se compose de cinq sections repliables
// (Le groupe, Courrier, Relances, Données, Zone sensible), la première ouverte ;
// elles s'ouvrent et se ferment au clavier (Entrée ou Espace sur le résumé) et
// fonctionnent sans JavaScript (éléments <details> natifs) ; aucun défilement
// horizontal en 360x640 et 390x844 (projets Playwright).

const IDS = ['groupe', 'courrier', 'relances', 'donnees', 'sensible'];

const isOpen = (page: Page, id: string) => page.locator(`details#${id}`).evaluate((d) => (d as HTMLDetailsElement).open);

async function checkAccordion(page: Page): Promise<void> {
  await expect(page.locator('main details.stg-acc')).toHaveCount(5);
  for (const id of IDS) await expect(page.locator(`details#${id} > summary`)).toBeVisible();
  expect(await isOpen(page, 'groupe'), 'la première section est ouverte').toBe(true);
  for (const id of IDS.slice(1)) expect(await isOpen(page, id), `« ${id} » repliée`).toBe(false);
}

test('réglages : cinq sections repliables, la première ouverte, au clavier', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  await loginAdmin(page, g);
  await page.goto(`/${g.slug}/admin/reglages`);
  await checkAccordion(page);
  await expectNoHorizontalScroll(page);

  for (const id of ['courrier', 'relances']) {
    const sum = page.locator(`details#${id} > summary`);
    await sum.focus();
    await page.keyboard.press('Enter');
    expect(await isOpen(page, id), `Entrée ouvre « ${id} »`).toBe(true);
    await expectNoHorizontalScroll(page);
    await page.keyboard.press('Enter');
    expect(await isOpen(page, id), `Entrée referme « ${id} »`).toBe(false);
    await page.keyboard.press('Space');
    expect(await isOpen(page, id), `Espace ouvre « ${id} »`).toBe(true);
    await page.keyboard.press('Space');
    expect(await isOpen(page, id), `Espace referme « ${id} »`).toBe(false);
  }

  // Toutes les sections ouvertes : toujours aucun débordement.
  for (const id of IDS) {
    if (!(await isOpen(page, id))) await page.locator(`details#${id} > summary`).click();
  }
  await expectNoHorizontalScroll(page);
});

test('réglages : les sections fonctionnent sans JavaScript', async ({ browser, page }, testInfo) => {
  const g = lane(testInfo).people;
  await loginAdmin(page, g);
  const state = await page.context().storageState();
  const context = await browser.newContext({
    ...testInfo.project.use,
    storageState: state,
    javaScriptEnabled: false,
  });
  try {
    const p = await context.newPage();
    await p.goto(`/${g.slug}/admin/reglages`);
    await checkAccordion(p);
    await p.locator('details#donnees > summary').click();
    expect(await isOpen(p, 'donnees'), 'ouverture sans script').toBe(true);
    await p.locator('details#donnees > summary').click();
    expect(await isOpen(p, 'donnees'), 'fermeture sans script').toBe(false);
    await expectNoHorizontalScroll(p);
  } finally {
    await context.close();
  }
});
