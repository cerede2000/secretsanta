import { expect, test, type Page } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared/mobile';

// Pre-requis : un groupe avec au moins quatre participants et aucun tirage fait
// existe ; son slug est dans E2E_ROLES_SLUG (defaut « e2e-roles ») et son
// secret administrateur dans E2E_ADMIN_SECRET. Routes attendues
// (internal/admin/roles, Routes()) : GET et POST /<slug>/admin/roles, plus la
// connexion /<slug>/admin/connexion.
const slug = process.env.E2E_ROLES_SLUG ?? 'e2e-roles';
const secret = process.env.E2E_ADMIN_SECRET ?? '';

async function login(page: Page): Promise<void> {
  await page.goto(`/${slug}/admin/connexion`);
  await page.locator('input[name="secret"]').fill(secret);
  await page.locator('form[data-access-admin] button[type="submit"]').click();
  await page.waitForURL((u) => !u.pathname.endsWith('/admin/connexion'));
}

async function openPanel(page: Page): Promise<void> {
  await login(page);
  const res = await page.goto(`/${slug}/admin/roles`);
  expect(res?.status(), `/${slug}/admin/roles doit exister`).toBe(200);
}

test.beforeEach(() => {
  test.skip(secret === '', 'E2E_ADMIN_SECRET non fourni');
});

test('panneau des roles : aucun defilement horizontal et cibles tactiles de 44 px', async ({ page }) => {
  await openPanel(page);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  for (const el of await page.locator('.roles select, .roles summary, .roles-pick').all()) {
    if (!(await el.isVisible())) continue;
    const box = await el.boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(43.5);
  }
});

test('panneau des roles : le recapitulatif ne donne que des comptes', async ({ page }) => {
  await openPanel(page);
  const counts = page.locator('.roles-counts > div');
  await expect(counts).toHaveCount(6);
  for (const dd of await page.locator('.roles-counts dd').all()) {
    expect(await dd.textContent()).toMatch(/^\d+$/);
  }
  await expect(page.locator('.roles-card').first().locator('.chip')).toBeVisible();
});

test('panneau des roles : le lot applique un role a plusieurs personnes', async ({ page }) => {
  page.on('dialog', () => {
    throw new Error('boite du navigateur interdite');
  });
  await openPanel(page);
  const picks = page.locator('[data-roles-pick]');
  await picks.nth(0).check();
  await picks.nth(1).check();
  await expect(page.locator('[data-roles-count]')).toContainText('2');
  await page.locator('#roles-bulk-role').selectOption('un_seul');
  await page.locator('.roles-bulk-apply').click();
  await expect(page.locator('[role="status"]')).toBeVisible();
  const selects = page.locator('.roles-row .roles-select');
  await expect(selects.nth(0)).toHaveValue('un_seul');
  await expect(selects.nth(1)).toHaveValue('un_seul');
  await expectNoHorizontalScroll(page);
  // remise a l'etat initial
  await picks.nth(0).check();
  await picks.nth(1).check();
  await page.locator('#roles-bulk-role').selectOption('normal');
  await page.locator('.roles-bulk-apply').click();
  await expect(selects.nth(0)).toHaveValue('normal');
});

test('panneau des roles : le selecteur d\'une ligne s\'envoie au changement', async ({ page }) => {
  await openPanel(page);
  const first = page.locator('.roles-row .roles-select').first();
  await first.selectOption('reception_seule');
  await expect(page.locator('[role="status"]')).toBeVisible();
  await expect(page.locator('.roles-row .roles-select').first()).toHaveValue('reception_seule');
  await page.locator('.roles-row .roles-select').first().selectOption('normal');
  await expect(page.locator('.roles-row .roles-select').first()).toHaveValue('normal');
});

test('panneau des roles : un lot sans personne affiche un message integre sous le champ', async ({ page }) => {
  page.on('dialog', () => {
    throw new Error('boite du navigateur interdite');
  });
  await openPanel(page);
  await page.locator('.roles-bulk-apply').click();
  const alert = page.locator('#roles-error');
  await expect(alert).toBeVisible();
  await expect(page.locator('#roles-bulk-role')).toHaveAttribute('aria-invalid', 'true');
  await expectNoHorizontalScroll(page);
});
