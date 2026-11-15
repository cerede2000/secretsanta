import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { TWO, openReveal, root } from '../core/harness';

const STYLES = ['gift', 'envelope', 'scratch'];

for (const motion of ['no-preference', 'reduce'] as const) {
  for (const seed of [1, 7, 11, 23, 42, 99]) {
    test(`séquence de deux révélations (graine ${seed}, ${motion}) : styles successifs différents, une confirmation par cible`, async ({ page }) => {
      await page.emulateMedia({ reducedMotion: motion });
      const h = await openReveal(page, { targets: TWO, seed });
      const open = page.locator('[data-role="stage"] button.reveal-open');
      const name = page.locator('[data-role="name"]');
      const next = page.locator('[data-role="next"]');

      const styles: string[] = [];
      for (const [i, who] of ['Léa', 'Noam'].entries()) {
        await expect(root(page)).toHaveAttribute('data-phase', 'ready');
        styles.push((await root(page).getAttribute('data-style')) ?? '');
        expect(STYLES).toContain(styles[i]);
        expect(h.confirms).toHaveLength(i);
        await open.click();
        await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
        await expect(name).toHaveText(who);
        await expect.poll(() => h.confirms.length).toBe(i + 1);
        expect(JSON.parse(h.confirmBodies[i])).toEqual({ target_id: i === 0 ? 't1' : 't2' });
        if (motion === 'reduce') {
          await expect(root(page)).toHaveAttribute('data-motion', 'reduced');
          expect(await page.locator('.reveal-confetti-piece').count()).toBe(0);
        }
        if (i === 0) {
          await expect(next).toBeVisible();
          await next.click();
        }
      }
      expect(styles[1]).not.toBe(styles[0]);
      await page.waitForTimeout(300);
      expect(h.confirms).toHaveLength(2);
      await expect(next).toBeHidden();
    });
  }
}

test('séquence mobile : aucun défilement horizontal, cibles tactiles >= 44 px à chaque étape', async ({ page }) => {
  await openReveal(page, { targets: TWO, seed: 7 });
  const open = page.locator('[data-role="stage"] button.reveal-open');
  for (let i = 0; i < 2; i++) {
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    await open.click();
    await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    if (i === 0) await page.locator('[data-role="next"]').click();
  }
});
