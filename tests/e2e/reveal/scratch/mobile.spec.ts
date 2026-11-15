import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { art, nameEl, openScratch, percent } from './harness';

test('carte : ni défilement horizontal ni cible tactile trop petite, avant et après', async ({ page }) => {
  await openScratch(page);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  const box = (await art(page).boundingBox())!;
  const vw = page.viewportSize()!.width;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(vw);
  expect(box.width).toBeGreaterThanOrEqual(44);
  expect(box.height).toBeGreaterThanOrEqual(44);

  await page.locator('[data-role="stage"] button.reveal-open').click();
  await expect(nameEl(page)).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('grattage au toucher : la cible se révèle sur un téléphone', async ({ browser, page: _p }, info) => {
  const vp = info.project.use.viewport!;
  const ctx = await browser.newContext({ viewport: vp, hasTouch: true, isMobile: true });
  const page = await ctx.newPage();
  const h = await openScratch(page);
  const cdp = await ctx.newCDPSession(page);
  const box = (await art(page).boundingBox())!;
  const x0 = box.x + 6;
  const x1 = box.x + box.width - 6;
  const touch = (type: string, x?: number, y?: number) =>
    cdp.send('Input.dispatchTouchEvent', { type, touchPoints: x === undefined ? [] : [{ x, y: y! }] } as any);
  for (let y = box.y + 8; y < box.y + box.height; y += 16) {
    await touch('touchStart', x0, y);
    for (let i = 1; i <= 6; i++) await touch('touchMove', x0 + ((x1 - x0) * i) / 6, y);
    await touch('touchEnd');
    if ((await percent(page)) >= 100) break;
  }
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await ctx.close();
});
