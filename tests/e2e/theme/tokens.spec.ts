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
