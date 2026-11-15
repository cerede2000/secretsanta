import { expect, test } from '@playwright/test';
import { TWO, openGift, openReveal, renderTemplate, root } from './harness';

test('le gabarit rendu : aucun style en ligne, data:, script en ligne ni gestionnaire on*', () => {
  for (const lang of ['fr', 'en']) {
    const html = renderTemplate(lang, { TargetsURL: '/t', ConfirmURL: '/c', LetterURL: '/l' });
    expect(html).not.toMatch(/\sstyle\s*=/i);
    expect(html).not.toMatch(/<style\b/i);
    expect(html).not.toMatch(/data:/i);
    expect(html).not.toMatch(/\son[a-z]+\s*=/i);
    expect(html).not.toMatch(/https?:\/\//i);
    for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/gi)) {
      expect(m[2].trim()).toBe('');
      expect(m[1]).toMatch(/type="module"/);
      expect(m[1]).toMatch(/src="\/static\/js\/reveal\/core\/[^"]+"/);
    }
  }
});

test('sous CSP stricte, tout le parcours (deux cibles, confettis) ne lève aucune violation', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('console', (m) => {
    if (/content security policy|refused to/i.test(m.text())) errors.push(m.text());
  });
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openReveal(page, { targets: TWO, seed: 3 });
  await openGift(page);
  await expect.poll(() => page.locator('.reveal-confetti-piece').count()).toBeGreaterThan(0);
  await page.locator('[data-role="next"]').click();
  await openGift(page);
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  expect(await page.evaluate(() => (window as any).__violations)).toEqual([]);
  expect(errors).toEqual([]);
  // Les feuilles de style de la page sont toutes locales.
  const sheets = await page.evaluate(() => [...document.styleSheets].map((s) => s.href));
  for (const href of sheets) expect(href).toMatch(/\/static\/css\//);
});
