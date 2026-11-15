import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { TWO, art, equivalent, nameEl, openEnvelope, seal } from './harness';

test('enveloppe : ni défilement horizontal ni cible tactile trop petite, avant et après', async ({ page }) => {
  await openEnvelope(page);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  const box = (await art(page).boundingBox())!;
  const vw = page.viewportSize()!.width;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(vw);
  expect(box.width).toBeGreaterThanOrEqual(44);
  expect(box.height).toBeGreaterThanOrEqual(44);
  const eq = (await equivalent(page).boundingBox())!;
  expect(eq.height).toBeGreaterThanOrEqual(44);
  expect(eq.width).toBeGreaterThanOrEqual(44);

  await seal(page).tap();
  await expect(nameEl(page)).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('enveloppe avec deux cibles : la première révélation et le bouton suivant restent dans l\'écran', async ({ page }) => {
  await openEnvelope(page, { targets: TWO });
  await expect(page.locator('[data-role="pill"]')).toBeVisible();
  await seal(page).tap();
  await expect(nameEl(page)).toHaveText('Léa');
  const next = page.locator('[data-role="next"]');
  await expect(next).toBeVisible();
  const box = (await next.boundingBox())!;
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  expect(box.height).toBeGreaterThanOrEqual(44);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('toucher le sceau sur un téléphone : la lettre sort et la confirmation part une fois', async ({ browser, page: _p }, info) => {
  const vp = info.project.use.viewport!;
  const ctx = await browser.newContext({ viewport: vp, hasTouch: true, isMobile: true });
  const page = await ctx.newPage();
  const h = await openEnvelope(page);
  const box = (await seal(page).boundingBox())!;
  const cdp = await ctx.newCDPSession(page);
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 2;
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] });
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await expect(nameEl(page)).toHaveText('Léa');
  await expect.poll(() => h.confirms.length).toBe(1);
  await page.waitForTimeout(400);
  expect(h.confirms.length).toBe(1);
  await expectNoHorizontalScroll(page);
  await ctx.close();
});
