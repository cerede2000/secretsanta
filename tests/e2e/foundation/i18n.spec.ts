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
    expect(text).not.toContain('Aller au contenu');
    // Pied de page en anglais, pas en francais.
    await expect(page.locator('footer')).toContainText('Gift exchange among friends');
    await expect(page.locator('footer')).not.toContainText('Échange de cadeaux entre proches');
    if (name === 'accueil') {
      for (const en of ['Welcome', 'Get started']) expect(text, en).toContain(en);
      for (const fr of ['Bienvenue', 'Commencer', 'Organisez votre']) expect(text, fr).not.toContain(fr);
    } else {
      const labels = (await page.locator('.admin-nav .admin-nav-label').allInnerTexts()).map((t) => t.trim());
      expect(labels).toEqual(['Dashboard', 'Participants', 'Draw', 'Sign-ups', 'More']);
      for (const fr of ['Tableau', 'Tirage', 'Inscriptions', 'Plus']) expect(labels, fr).not.toContain(fr);
      await expect(page.locator('.admin-nav')).toHaveAttribute('aria-label', 'Organiser navigation');
    }
  });
}

for (const current of ['fr', 'en'] as const) {
  test(`le selecteur de langue liste les langues chargees par leur nom natif (${current})`, async ({ page, context, baseURL }) => {
    if (current === 'en') await context.addCookies([{ name: 'lang', value: 'en', url: baseURL! }]);
    await page.goto(HOME_PATH);
    const footer = page.locator('footer');
    await expect(footer).toBeVisible();
    const select = footer.locator('form select[name="lang"]');
    await expect(select).toHaveCount(1);
    const options = await select.locator('option').evaluateAll((os) =>
      os.map((o) => ({ value: (o as HTMLOptionElement).value, name: o.textContent!.trim() })),
    );
    expect(options.map((o) => o.name).sort()).toEqual(['English', 'Français']);
    expect(options.find((o) => o.name === 'English')!.value).toBe('en');
    expect(options.find((o) => o.name === 'Français')!.value).toBe('fr');
    await expect(select).toHaveValue(current);
    await expect(select.locator('option:checked')).toHaveText(current === 'en' ? 'English' : 'Français');
    await expect(footer.locator('form button[type="submit"]')).toBeAttached();
  });
}
