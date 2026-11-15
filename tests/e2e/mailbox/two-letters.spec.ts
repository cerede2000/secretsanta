import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared';
import { openMailbox, TWO_LETTERS } from './harness';

test('boîte à deux lettres reçues : deux entrées distinctes, sans défilement horizontal, cibles >= 44 px', async ({ page }) => {
  await openMailbox(page, TWO_LETTERS);
  const entries = page.locator('.mb-recv');
  await expect(entries).toHaveCount(2);
  await expect(entries.nth(0)).toHaveText('Lettre reçue · cadeau 1 sur 2');
  await expect(entries.nth(1)).toHaveText('Lettre reçue · cadeau 2 sur 2');
  const hrefs = await page.locator('a.mb-entry').evaluateAll((els) => els.map((e) => e.getAttribute('href')));
  expect(hrefs).toEqual(['/s/lettre', '/s/lettre-recue/1', '/s/lettre-recue/2']);
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('a.mb-entry'));
});

test('la note sur la deuxième lettre n\'apparaît que pour deux cibles', async ({ page }) => {
  await openMailbox(page, TWO_LETTERS);
  await expect(page.locator('.mb-note')).toHaveCount(1);
  await page.unroute('**/__mailbox_harness__');
  await openMailbox(page, { State: 'sent', Received: [{ N: 1, Total: 1, Written: true, URL: '/s/lettre-recue/1' }] });
  await expect(page.locator('.mb-note')).toHaveCount(0);
  await expect(page.locator('.mb-recv')).toHaveText('Lettre reçue');
});
