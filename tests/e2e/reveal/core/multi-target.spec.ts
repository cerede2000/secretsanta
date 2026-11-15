import { expect, test } from '@playwright/test';
import { TWO, openGift, openReveal, root } from './harness';

for (const seed of [1, 2, 3, 4, 5]) {
  test(`deux cibles (graine ${seed}) : styles différents, une confirmation par cible, séquence ordonnée`, async ({ page }) => {
    const h = await openReveal(page, { targets: TWO, seed });
    const name = page.locator('[data-role="name"]');
    const next = page.locator('[data-role="next"]');
    const pill = page.locator('[data-role="pill"]');

    await expect(root(page)).toHaveAttribute('data-phase', 'ready');
    await expect(pill).toHaveText('Cadeau 1 sur 2');
    const first = await root(page).getAttribute('data-style');
    expect(['gift', 'envelope', 'scratch']).toContain(first);
    // La seconde ne se lance pas avant la première.
    await expect(next).toBeHidden();
    expect(h.confirms).toHaveLength(0);

    await openGift(page);
    await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
    await expect(name).toHaveText('Léa');
    await expect.poll(() => h.confirms.length).toBe(1);
    expect(JSON.parse(h.confirmBodies[0])).toEqual({ target_id: 't1' });
    expect(h.confirms[0].method()).toBe('POST');
    expect(await h.confirms[0].headerValue('x-csrf-token')).toBe('csrf-test-token');
    await expect(next).toBeVisible();
    await expect(next).toHaveText('Découvrir mon second cadeau');

    await next.click();
    await expect(root(page)).toHaveAttribute('data-phase', 'ready');
    await expect(pill).toHaveText('Cadeau 2 sur 2');
    const second = await root(page).getAttribute('data-style');
    expect(second).not.toBe(first);
    await expect(page.locator('[data-role="result"]')).toBeHidden();
    expect(h.confirms).toHaveLength(1);

    await openGift(page);
    await expect(name).toHaveText('Noam');
    await expect.poll(() => h.confirms.length).toBe(2);
    expect(JSON.parse(h.confirmBodies[1])).toEqual({ target_id: 't2' });
    await expect(next).toBeHidden();
    await page.waitForTimeout(300);
    expect(h.confirms).toHaveLength(2);
  });
}

test('une confirmation refusée n\'ouvre pas la seconde cible et se rejoue sur demande', async ({ page }) => {
  const h = await openReveal(page, { targets: TWO, seed: 1, confirmStatus: 500 });
  await openGift(page);
  await expect(page.locator('[data-role="error"]')).toBeVisible();
  await expect(page.locator('[data-role="next"]')).toBeHidden();
  await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
  await page.locator('[data-role="retry"]').click();
  await expect.poll(() => h.confirms.length).toBe(2);
});
