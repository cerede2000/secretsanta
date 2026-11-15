import { expect, type Locator, type Page } from '@playwright/test';

/** Taille minimale d'une zone tactile, en pixels CSS. */
export const MIN_TOUCH_TARGET = 44;

/** Verifie l'absence de defilement horizontal de la page. */
export async function expectNoHorizontalScroll(page: Page): Promise<void> {
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(
    scrollWidth,
    `defilement horizontal : scrollWidth=${scrollWidth} > clientWidth=${clientWidth}`,
  ).toBeLessThanOrEqual(clientWidth);
}

/** Verifie que chaque element visible du locator mesure au moins 44x44 px. */
export async function expectTouchTargets(
  locator: Locator,
  min: number = MIN_TOUCH_TARGET,
): Promise<void> {
  const count = await locator.count();
  expect(count, 'aucune cible tactile trouvee').toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const item = locator.nth(i);
    if (!(await item.isVisible())) continue;
    const box = await item.boundingBox();
    expect(box, `cible ${i} sans boite`).not.toBeNull();
    const label = (await item.textContent())?.trim() || `#${i}`;
    // Tolerance de 0,5 px pour l'arrondi des sous-pixels.
    expect(box!.width, `largeur de « ${label} »`).toBeGreaterThanOrEqual(min - 0.5);
    expect(box!.height, `hauteur de « ${label} »`).toBeGreaterThanOrEqual(min - 0.5);
  }
}

/** Selecteur des elements interactifs de la page. */
export const INTERACTIVE = 'button, [role="button"], input[type="submit"], a.button';

/** Verifie toutes les cibles interactives visibles de la page. */
export async function expectAllInteractiveTargets(page: Page): Promise<void> {
  await expectTouchTargets(page.locator(INTERACTIVE));
}

/** Verifie qu'un element est dans le tiers inferieur de la fenetre. */
export async function expectInLowerThird(page: Page, locator: Locator): Promise<void> {
  const box = await locator.boundingBox();
  expect(box, 'element sans boite').not.toBeNull();
  const height = page.viewportSize()!.height;
  expect(box!.y, 'element hors du tiers inferieur').toBeGreaterThanOrEqual((height * 2) / 3 - 0.5);
}
