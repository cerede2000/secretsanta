import { expect, test } from '@playwright/test';
import { expectInLowerThird, expectNoHorizontalScroll, expectTouchTargets } from '../_shared';
import { ADMIN_PATH } from './paths';

test.describe('telephone : barre basse', () => {
  test('5 entrees dans le tiers inferieur, cibles de 44 px, entree active marquee', async ({ page }) => {
    const vp = page.viewportSize()!;
    test.skip(vp.width >= 1024, 'projet mobile uniquement');
    await page.goto(ADMIN_PATH);
    const items = page.locator('.admin-nav .admin-nav-item');
    await expect(items).toHaveCount(5);
    await expect(items.locator('svg')).toHaveCount(5);
    await expectTouchTargets(items);
    for (let i = 0; i < 5; i++) await expectInLowerThird(page, items.nth(i));
    await expectNoHorizontalScroll(page);

    const active = page.locator('.admin-nav [aria-current="page"]');
    await expect(active).toHaveCount(1);
    const other = items.locator('xpath=self::*[not(@aria-current)]').first();
    const style = async (l: typeof active) =>
      l.evaluate((el) => {
        const s = getComputedStyle(el);
        return { weight: Number(s.fontWeight), borderTop: s.borderTopWidth };
      });
    const a = await style(active);
    const o = await style(other);
    expect(a.weight, 'gras en plus de la couleur').toBeGreaterThan(o.weight);
    expect(parseFloat(a.borderTop), 'trait en plus de la couleur').toBeGreaterThan(parseFloat(o.borderTop));
  });
});

test.describe('1024 px : barre laterale', () => {
  test.use({ viewport: { width: 1024, height: 768 }, isMobile: false });

  test('la barre laterale remplace la barre basse', async ({ page }) => {
    await page.goto(ADMIN_PATH);
    const nav = page.locator('.admin-nav');
    const items = nav.locator('.admin-nav-item');
    await expect(items).toHaveCount(5);
    const box = (await nav.boundingBox())!;
    expect(box.x).toBeLessThanOrEqual(1);
    expect(box.width).toBeLessThan(400);
    expect(box.height).toBeGreaterThanOrEqual(768 - 1);
    await expectNoHorizontalScroll(page);
  });
});
