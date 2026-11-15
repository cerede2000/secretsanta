import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared';
import { openMailbox, root, TWO_LETTERS } from './harness';

const states = ['empty', 'draft', 'sent', 'read'] as const;

for (const state of states) {
  test(`boîte aux lettres (${state}) : ni défilement horizontal ni cible trop petite`, async ({ page }) => {
    await openMailbox(page, { State: state });
    await expect(root(page)).toHaveAttribute('data-state', state);
    await expectNoHorizontalScroll(page);
    await expectTouchTargets(page.locator('a.mb-entry, a.mb-back'));
  });
}

test('réception seule : une seule entrée, « Ma lettre »', async ({ page }) => {
  await openMailbox(page, { State: 'empty', ReceptionOnly: true });
  await expect(page.locator('.mb-entry')).toHaveCount(1);
  await expectNoHorizontalScroll(page);
});

test('le bloc de nuit couvre toute la largeur, sans arrondi', async ({ page }) => {
  await openMailbox(page, { State: 'sent' });
  const box = await root(page).boundingBox();
  const vw = page.viewportSize()!.width;
  expect(Math.round(box!.x)).toBe(0);
  expect(Math.round(box!.width)).toBe(vw);
  expect(await root(page).evaluate((el) => getComputedStyle(el).borderRadius)).toBe('0px');
});

test('anglais : aucun texte français', async ({ page }) => {
  await openMailbox(page, { ...TWO_LETTERS, Lang: 'en' });
  await expect(page.locator('h1')).toHaveText('Your mailbox');
  await expect(page.locator('.mb-recv').first()).toHaveText('Letter received · gift 1 of 2');
});
