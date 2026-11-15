import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { TWO, openGift, openReveal, root } from './harness';

test('la page de la seconde révélation : ni défilement horizontal ni cible tactile trop petite', async ({ page }) => {
  await openReveal(page, { targets: TWO, seed: 4 });
  await openGift(page);
  const next = page.locator('[data-role="next"]');
  await expect(next).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);

  await next.click();
  await expect(page.locator('[data-role="pill"]')).toHaveText('Cadeau 2 sur 2');
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);

  await openGift(page);
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  await expect(page.locator('[data-role="name"]')).toHaveText('Noam');
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});
