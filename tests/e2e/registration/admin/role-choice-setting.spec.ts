import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../../_shared/admin';
import { lane, loginAdmin } from '../../_shared/seed';

// F11e.AC3 et AC4 (administration) : le reglage « Proposer aux inscrits de choisir
// leur role » est desactive par defaut, modifiable, sans defilement horizontal et
// avec des zones tactiles de 44 px en 360x640 et 390x844.
// Donnees : groupe « roles » du couloir (le test remet le reglage a l'etat initial).
test('reglage du choix de role : desactive par defaut, modifiable, mobile', async ({ page }, testInfo) => {
  const g = lane(testInfo).roles;
  const dialogs = trackDialogs(page);
  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/inscriptions`);
  const sw = page.locator('.regadm-role-switch');
  await expect(page.locator('#regadm-role')).toHaveText('Proposer aux inscrits de choisir leur rôle');
  await expect(sw).toHaveAttribute('aria-checked', 'false');
  await expectCleanAdminPage(page, dialogs);
  const box = await sw.boundingBox();
  expect(box!.width).toBeGreaterThanOrEqual(43.5);
  expect(box!.height).toBeGreaterThanOrEqual(43.5);

  await sw.click();
  await expect(page.locator('.regadm-role-switch')).toHaveAttribute('aria-checked', 'true');
  await expectCleanAdminPage(page, dialogs);
  await page.locator('.regadm-role-switch').click();
  await expect(page.locator('.regadm-role-switch')).toHaveAttribute('aria-checked', 'false');
  expect(dialogs()).toBe(0);
});
