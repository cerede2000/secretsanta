import { expect, test } from '@playwright/test';
import { ONE, art, nameEl, openGift } from './harness';

async function center(page: import('@playwright/test').Page) {
  const box = (await art(page).boundingBox())!;
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

test('appui long : le cadeau s\'ouvre et la confirmation part une seule fois', async ({ page }) => {
  const h = await openGift(page);
  const { x, y } = await center(page);
  await page.mouse.move(x, y);
  await page.mouse.down();
  await expect(page.locator('.gift')).toHaveClass(/is-holding/);
  await page.waitForTimeout(1800);
  await page.mouse.up();
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await page.waitForTimeout(500);
  expect(h.confirms.length).toBe(1);
  expect(JSON.parse(h.confirmBodies[0])).toEqual({ target_id: 't1' });
  expect(h.confirms[0].headers()['x-csrf-token']).toBe('csrf-test-token');
});

test('glissement : le cadeau s\'ouvre et la confirmation part une seule fois', async ({ page }) => {
  const h = await openGift(page);
  const { x, y } = await center(page);
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 60, y, { steps: 4 });
  await page.mouse.move(x + 140, y, { steps: 4 });
  await page.mouse.up();
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await page.waitForTimeout(500);
  expect(h.confirms.length).toBe(1);
});

test('bouton équivalent (clic) : même résultat, une seule confirmation', async ({ page }) => {
  const h = await openGift(page);
  const btn = page.locator('[data-role="stage"] button.reveal-open');
  await btn.click();
  await btn.click({ timeout: 300 }).catch(() => {});
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await page.waitForTimeout(500);
  expect(h.confirms.length).toBe(1);
});

test('bouton équivalent au clavier : même résultat', async ({ page }) => {
  const h = await openGift(page);
  await page.locator('[data-role="stage"] button.reveal-open').focus();
  await page.keyboard.press('Enter');
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
});

test('un appui trop court ne suffit pas : le ruban se renoue, rien n\'est confirmé', async ({ page }) => {
  const h = await openGift(page);
  const { x, y } = await center(page);
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.waitForTimeout(500);
  await page.mouse.up();
  await expect(page.locator('.gift')).not.toHaveClass(/is-holding/);
  await page.waitForTimeout(1500);
  await expect(page.locator('[data-role="result"]')).toBeHidden();
  expect(h.confirms.length).toBe(0);
  await expect(page.locator('[data-role="stage"] button.reveal-open')).toBeVisible();
});
