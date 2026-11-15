import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from '../_shared/mobile';

const slug = process.env.E2E_GROUP_SLUG ?? 'e2e-acces';

// L'accueil nocturne et le bandeau vert de creation vont de bord a bord,
// sans rayon, comme dans la maquette.
const BANDS: Array<[string, string, string]> = [
  ['fond nocturne de l\'accueil du groupe', `/${slug}`, '.groups-home'],
  ['bandeau vert de la creation', '/creer', '.groups-create-head'],
];

for (const width of [360, 390]) {
  for (const [name, path, selector] of BANDS) {
    test(`${name} : bord a bord a ${width} px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      await page.goto(path);
      const box = await page.locator(selector).boundingBox();
      expect(box, `${selector} sans boite`).not.toBeNull();
      expect(Math.round(box!.x)).toBe(0);
      expect(Math.round(box!.x + box!.width)).toBe(width);
      await expect(page.locator(selector)).toHaveCSS('border-top-left-radius', '0px');
      await expect(page.locator(selector)).toHaveCSS('border-bottom-right-radius', '0px');
      await expectNoHorizontalScroll(page);
    });
  }
}

test('le bandeau de creation porte l\'etoile doree', async ({ page }) => {
  await page.goto('/creer');
  const star = page.locator('.groups-create-head svg.groups-create-star');
  await expect(star).toHaveCount(1);
  await expect(star).toHaveAttribute('aria-hidden', 'true');
  await expect(star.locator('path')).toHaveAttribute('fill', '#ffd700');
});
