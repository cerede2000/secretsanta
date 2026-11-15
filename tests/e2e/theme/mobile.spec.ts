import { expect, test } from '@playwright/test';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { openHarness } from './harness';

// Elements interactifs du theme : boutons (toutes variantes), liens en bouton, champs et listes.
const INTERACTIVE = 'button, a.btn, [role="button"], input:not([type="hidden"]), select, textarea';

// Composants attendus dans la page d'essai : le test echoue si l'un d'eux disparait.
const COMPONENTS = [
  '#title', '#card', '#night', '#night-title', '#link',
  '#b-red', '#b-green', '#b-ghost', '#b-danger', '#b-gold', '#b-ghost-light', '#b-off',
  '#f', '#err', '#c-ok', '#c-warn', '#c-error',
];

test('les composants du theme sont tous presents et visibles', async ({ page }) => {
  await openHarness(page);
  for (const sel of COMPONENTS) {
    await expect(page.locator(sel), sel).toBeVisible();
  }
});

test('la page du theme n\'a aucun defilement horizontal', async ({ page }) => {
  await openHarness(page);
  await expectNoHorizontalScroll(page);
});

test('chaque element interactif du theme mesure au moins 44x44 px', async ({ page }) => {
  await openHarness(page);
  const count = await page.locator(INTERACTIVE).count();
  expect(count, 'boutons des 7 variantes + champ').toBeGreaterThanOrEqual(8);
  await expectTouchTargets(page.locator(INTERACTIVE));
});

test('un lien style en bouton et une liste mesurent aussi 44x44 px', async ({ page }) => {
  await openHarness(page);
  await page.evaluate(() => {
    const a = document.createElement('a');
    a.className = 'btn btn-red';
    a.href = '#';
    a.textContent = 'Lien bouton';
    const s = document.createElement('select');
    s.className = 'field';
    s.innerHTML = '<option>Un</option>';
    document.body.append(a, s);
  });
  await expectTouchTargets(page.locator('a.btn, select'));
  await expectNoHorizontalScroll(page);
});
