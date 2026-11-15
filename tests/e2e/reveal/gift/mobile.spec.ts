import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { art, nameEl, openGift } from './harness';

test('cadeau : ni défilement horizontal ni cible tactile trop petite, avant et après', async ({ page }) => {
  await openGift(page);
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

test('appui long au toucher : le cadeau s\'ouvre sur un téléphone', async ({ browser, page: _p }, info) => {
  const vp = info.project.use.viewport!;
  const ctx = await browser.newContext({ viewport: vp, hasTouch: true, isMobile: true });
  const page = await ctx.newPage();
  const h = await openGift(page);
  const box = (await art(page).boundingBox())!;
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  const cdp = await ctx.newCDPSession(page);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
  await page.waitForTimeout(1800);
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await ctx.close();
});
