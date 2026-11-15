import { expect, test } from '@playwright/test';
import { expectInLowerThird, expectNoHorizontalScroll, expectTouchTargets } from '../../_shared';
import { expectTouchTargetsOk, open } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';

// F10.AC10 : sur les pages d'administration, la barre de navigation basse et la
// feuille « Plus » restent dans le tiers inferieur, toutes les cibles font au
// moins 44 px, aucun defilement horizontal en 360x640 et 390x844 ; en 1024 px
// la barre laterale remplace la barre basse. Donnees : _shared/seed.ts.
const PAGES = ['admin', 'admin/participants', 'admin/tirage', 'admin/inscriptions', 'admin/plus'];

for (const rel of PAGES) {
  test(`${rel} : barre basse dans le tiers inferieur, 5 entrees de 44 px, une seule active`, async ({ page }, testInfo) => {
    const g = lane(testInfo).people;
    await loginAdmin(page, g);
    await open(page, `/${g.slug}/${rel}`);
    const items = page.locator('.admin-nav .admin-nav-item');
    await expect(items).toHaveCount(5);
    await expectTouchTargets(items);
    for (let i = 0; i < 5; i++) await expectInLowerThird(page, items.nth(i));
    await expect(page.locator('.admin-nav [aria-current="page"]')).toHaveCount(1);
    // La barre reste visible, collee au bas de la fenetre, meme apres un defilement.
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await expectInLowerThird(page, items.first());
    await expectNoHorizontalScroll(page);
  });
}

test('feuille « Plus » : six entrees dont la deconnexion separee, tout atteignable sans defilement', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/plus`);
  await expect(page.locator('.admin-menu-item')).toHaveCount(6);
  const logout = page.locator('.admin-menu-sensitive .admin-menu-logout');
  await expect(logout).toBeVisible();
  // La deconnexion est visuellement separee des cinq autres entrees.
  const [last, out] = [await page.locator('.admin-menu-item').nth(4).boundingBox(), await logout.boundingBox()];
  expect(out!.y - (last!.y + last!.height), 'espace entre les entrees et la deconnexion').toBeGreaterThan(8);
  // La feuille tient dans la fenetre (rien a defiler pour atteindre une entree) et la barre est sous elle.
  const vh = page.viewportSize()!.height;
  for (const el of await page.locator('.admin-menu-item').all()) {
    const b = (await el.boundingBox())!;
    expect(b.y + b.height, 'entree hors de la fenetre').toBeLessThanOrEqual(vh);
  }
  await expectInLowerThird(page, page.locator('.admin-nav-item[aria-current="page"]'));
  await expectTouchTargets(page.locator('.admin-menu-item'));
  await expectTouchTargetsOk(page);
  await expectNoHorizontalScroll(page);
});

test.describe('1024 px : barre laterale', () => {
  test.use({ viewport: { width: 1024, height: 768 }, isMobile: false, hasTouch: false });

  for (const rel of PAGES) {
    test(`${rel} : la barre laterale remplace la barre basse`, async ({ page }, testInfo) => {
      const g = lane(testInfo).people;
      await loginAdmin(page, g);
      await open(page, `/${g.slug}/${rel}`);
      const nav = page.locator('.admin-nav');
      await expect(nav.locator('.admin-nav-item')).toHaveCount(5);
      const box = (await nav.boundingBox())!;
      expect(box.x, 'barre a gauche').toBeLessThanOrEqual(0.5);
      expect(box.width, 'barre etroite').toBeLessThan(300);
      expect(box.height, 'barre sur toute la hauteur').toBeGreaterThan(500);
      await expectNoHorizontalScroll(page);
    });
  }
});
