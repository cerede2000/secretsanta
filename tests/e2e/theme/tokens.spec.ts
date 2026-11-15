import { expect, test } from '@playwright/test';
import { openHarness } from './harness';

test('le theme expose rouge, vert sapin, or et le degrade bleu nuit en variables', async ({ page }) => {
  await openHarness(page);
  const v = await page.evaluate(() => {
    const s = getComputedStyle(document.documentElement);
    const get = (n: string) => s.getPropertyValue(n).trim().toLowerCase();
    return { red: get('--red'), green: get('--green'), gold: get('--gold'), n1: get('--night-1'), n2: get('--night-2'), grad: get('--night-gradient'), frost: get('--frost') };
  });
  expect(v.red).toBe('#c41e3a');
  expect(v.green).toBe('#165b33');
  expect(v.gold).toBe('#ffd700');
  expect(v.n1).toBe('#0f1b3d');
  expect(v.n2).toBe('#1c2f6b');
  expect(v.frost).toBe('#eaf4ee');
  expect(v.grad).toContain('linear-gradient');
  expect(v.grad).toContain('#0f1b3d');
  expect(v.grad).toContain('#1c2f6b');
});

// Deux cartes de la fiche « tokens » : festive (givre, rayon 20, ombre bleutee) et admin (blanche, rayon 14, contour, sans ombre).
test('la carte festive et la carte admin correspondent a la fiche des tokens', async ({ page }) => {
  await openHarness(page);
  const m = await page.evaluate(() => {
    const read = (id: string) => {
      const s = getComputedStyle(document.getElementById(id)!);
      return { bg: s.backgroundColor, radius: s.borderTopLeftRadius, shadow: s.boxShadow, padding: parseFloat(s.paddingTop) };
    };
    return { festive: read('card'), admin: read('card-admin') };
  });
  expect(m.festive.bg).toBe('rgb(234, 244, 238)'); // #eaf4ee
  expect(m.festive.radius).toBe('20px');
  expect(m.festive.shadow).toContain('rgba(8, 14, 40, 0.22) 0px 6px 20px');
  expect(m.festive.shadow).toContain('rgb(197, 221, 208) 0px 0px 0px 1px inset'); // filet #c5ddd0

  expect(m.admin.bg).toBe('rgb(255, 255, 255)');
  expect(m.admin.radius).toBe('14px');
  expect(m.admin.shadow).toBe('rgb(197, 221, 208) 0px 0px 0px 1px inset'); // simple contour, aucune ombre portee
  expect(m.admin.padding).toBeLessThan(m.festive.padding); // plus dense
});
