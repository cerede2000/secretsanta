import { expect, test } from '@playwright/test';
import { ONE, nameEl, openScratch, percent, scratchRows } from './harness';

test('moins de 50 % gratté : rien n\'est révélé ni confirmé', async ({ page }) => {
  const h = await openScratch(page);
  await scratchRows(page, 0, 0.3);
  const p = await percent(page);
  expect(p).toBeGreaterThan(0);
  expect(p).toBeLessThan(50);
  await page.waitForTimeout(800);
  await expect(page.locator('[data-role="result"]')).toBeHidden();
  expect(h.confirms.length).toBe(0);
});

test('à 50 % gratté : la cible se révèle, une seule confirmation', async ({ page }) => {
  const h = await openScratch(page);
  await scratchRows(page, 0, 0.3);
  expect(await percent(page)).toBeLessThan(50);
  await scratchRows(page, 0.3, 1);
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await page.waitForTimeout(500);
  expect(h.confirms.length).toBe(1);
  expect(JSON.parse(h.confirmBodies[0])).toEqual({ target_id: ONE[0].id });
  expect(h.confirms[0].headers()['x-csrf-token']).toBe('csrf-test-token');
});

test('bouton équivalent (clic) : même résultat, une seule confirmation', async ({ page }) => {
  const h = await openScratch(page);
  const btn = page.locator('[data-role="stage"] button.reveal-open');
  await btn.click();
  await btn.click({ timeout: 300 }).catch(() => {});
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await page.waitForTimeout(500);
  expect(h.confirms.length).toBe(1);
});

test('bouton équivalent au clavier : même résultat', async ({ page }) => {
  const h = await openScratch(page);
  await page.locator('[data-role="stage"] button.reveal-open').focus();
  await page.keyboard.press('Enter');
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
});

test('gratter puis le bouton : une seule confirmation', async ({ page }) => {
  const h = await openScratch(page);
  await scratchRows(page, 0, 1);
  await page.locator('[data-role="stage"] button.reveal-open').click({ timeout: 300 }).catch(() => {});
  await expect(nameEl(page)).toHaveText('Léa');
  await page.waitForTimeout(600);
  expect(h.confirms.length).toBe(1);
});
