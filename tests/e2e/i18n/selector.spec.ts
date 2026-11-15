import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared';
import { HOME_PATH } from '../foundation/paths';

const SELECT = 'footer .lang-selector-select';
const APPLY = 'footer .lang-selector-apply';

test('selecteur de langue : pouce, sans defilement horizontal, cibles de 44 px', async ({ page }) => {
  await page.goto(HOME_PATH);
  await page.locator('footer').scrollIntoViewIfNeeded();
  await expect(page.locator(SELECT)).toBeVisible();
  await expect(page.locator(`${SELECT} option`)).toContainText(['English', 'Français']);
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator(`${SELECT}, ${APPLY}`));
});

test('selecteur de langue : le changement pose le cookie lang et traduit la page', async ({ page, context }) => {
  await page.goto(HOME_PATH);
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
  await page.selectOption(SELECT, 'en');
  await Promise.all([page.waitForURL(/.*/), page.locator(APPLY).click()]);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  expect(await page.locator('body').innerText()).toContain('Skip to content');
  const cookie = (await context.cookies()).find((c) => c.name === 'lang');
  expect(cookie?.value).toBe('en');
  await expectNoHorizontalScroll(page);
});
