import { expect, test } from '@playwright/test';
import { ADMIN_PATH, HOME_PATH } from './paths';

for (const [name, path] of [['accueil', HOME_PATH], ['admin', ADMIN_PATH]] as const) {
  test(`${name} : aucun texte brut, aucune cle affichee`, async ({ page }) => {
    await page.goto(path);
    const text = await page.locator('body').innerText();
    expect(text, 'cle de texte non traduite affichee').not.toMatch(/\blayout\.[a-z_.]+/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
  });

  test(`${name} : affichage en anglais avec le cookie lang=en`, async ({ page, context, baseURL }) => {
    await context.addCookies([{ name: 'lang', value: 'en', url: baseURL! }]);
    await page.goto(path);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    const text = await page.locator('body').innerText();
    expect(text).not.toMatch(/\blayout\.[a-z_.]+/);
    expect(text).toContain('Skip to content');
  });
}

test('le selecteur de langue est present dans le pied de page', async ({ page }) => {
  await page.goto(HOME_PATH);
  await expect(page.locator('footer')).toBeVisible();
  await expect(page.locator('footer').locator('select, button, a, form').first()).toBeAttached();
});
