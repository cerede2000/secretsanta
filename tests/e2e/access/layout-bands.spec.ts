import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll } from '../_shared/mobile';

const slug = process.env.E2E_GROUP_SLUG ?? 'e2e-acces';

// Le fond nocturne (connexion) et le bandeau vert (connexion administrateur)
// doivent aller de bord a bord, sans bande claire a droite.
const BANDS: Array<[string, string, string]> = [
  ['fond nocturne de la connexion', `/${slug}/connexion`, '.access-night'],
  ['bandeau vert de la connexion administrateur', `/${slug}/admin/connexion`, '.access-admin-head'],
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
      await expectNoHorizontalScroll(page);
    });
  }
}

// Etagement de la neige : jamais au-dessus des champs et boutons, absente des
// ecrans clairs.
test('la neige reste sous le contenu de la connexion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await page.goto(`/${slug}/connexion`);
  await expect(page.locator('.snow')).toHaveCSS('display', 'block');
  await expect(page.locator('.snow')).toHaveCSS('pointer-events', 'none');
  for (const sel of ['input[name="code"]', 'button.access-submit', '.access-card']) {
    const top = await page.locator(sel).first().evaluate((el) => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      return { inside: !!hit && (el === hit || el.contains(hit)), snow: !!hit?.closest('.snow') };
    });
    expect(top.snow, `${sel} recouvert par la neige`).toBe(false);
    expect(top.inside, `${sel} non atteignable`).toBe(true);
  }
  // Ordre de peinture : le contenu de la carte est au-dessus de la neige.
  const z = await page.evaluate(() => {
    const snow = getComputedStyle(document.querySelector('.snow')!).zIndex;
    const card = getComputedStyle(document.querySelector('.access-night > .access-card')!).zIndex;
    return { snow: Number(snow), card: Number(card) };
  });
  expect(z.card).toBeGreaterThan(z.snow);
});

for (const [name, path] of [
  ['la connexion administrateur', `/${slug}/admin/connexion`],
  ['la creation de groupe', '/creer'],
] as const) {
  test(`pas de neige sur ${name} (ecran clair)`, async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto(path);
    const display = await page.evaluate(() => {
      const el = document.querySelector('.snow');
      return el ? getComputedStyle(el).display : 'absent';
    });
    expect(['none', 'absent']).toContain(display);
  });
}
