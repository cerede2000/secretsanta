import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';
import { lane, loginPerson, person } from '../_shared/seed';

// F4d.AC7 et AC9 : une personne au role reception_seule, avant le tirage
// (groupe « draft ») puis apres (groupe « revealed »), donnees de _shared/seed.ts.
for (const kind of ['draft', 'revealed'] as const) {
  test(`reception seule (${kind}) : accueil sans cible ni compte a rebours, lettre a ecrire, mobile`, async ({ page }, testInfo) => {
    const g = lane(testInfo)[kind];
    const slug = g.slug;
    await loginPerson(page, person(g, 'reception_seule'));
    await page.goto(`/${slug}/participant`);
    await expect(page.locator('.draw-reception')).toBeVisible();
    await expect(page.locator('[data-countdown]')).toHaveCount(0);
    await expect(page.locator('.draw-pref')).toHaveCount(0);
    await expect(page.locator(`a[href="/${slug}/decouverte"]`)).toHaveCount(0);
    await expect(page.locator(`a[href="/${slug}/lettre"]`).first()).toBeVisible();
    expect(await page.locator('[style]').count()).toBe(0);
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    for (const a of await page.locator('.draw-actions a').all()) {
      const box = await a.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(43.5);
    }
  });
}
