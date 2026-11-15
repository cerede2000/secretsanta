import fs from 'node:fs';
import { expect, test } from '@playwright/test';
import { TEMPLATE, catalog, openGift, openReveal, root, templateKeys, ONE } from './harness';

const params = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

test('catalogues fr et en : mêmes clés, mêmes paramètres, préfixe du module', () => {
  const fr = catalog('fr');
  const en = catalog('en');
  expect(Object.keys(en).sort()).toEqual(Object.keys(fr).sort());
  for (const k of Object.keys(fr)) {
    expect(k.startsWith('reveal.core.'), k).toBe(true);
    expect(params(en[k]), `paramètres de ${k}`).toEqual(params(fr[k]));
    expect(fr[k].trim(), k).not.toBe('');
    expect(en[k].trim(), k).not.toBe('');
  }
});

test('le gabarit n\'utilise que des clés existantes et n\'écrit aucun texte brut', () => {
  const src = fs.readFileSync(TEMPLATE, 'utf8');
  const keys = templateKeys(src);
  expect(keys.length).toBeGreaterThan(10);
  const fr = catalog('fr');
  const en = catalog('en');
  for (const k of keys) {
    expect(fr[k], `fr : ${k}`).toBeTruthy();
    expect(en[k], `en : ${k}`).toBeTruthy();
  }
  // Retire commentaires, actions, balises : il ne doit rester aucun texte.
  const text = src
    .replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '')
    .replace(/\{\{[^}]*\}\}/g, '')
    .replace(/<[^>]*>/g, '')
    .trim();
  expect(text).toBe('');
});

for (const lang of ['fr', 'en']) {
  test(`rendu en ${lang} : aucune clé brute, textes fournis par attributs data`, async ({ page }) => {
    await openReveal(page, { lang, targets: ONE, seed: 1 });
    const cat = catalog(lang);
    const dataset = await root(page).evaluate((el) => ({ ...(el as HTMLElement).dataset }));
    expect(dataset.tTitleReady).toBe(cat['reveal.core.title.ready']);
    expect(dataset.tOpen).toBe(cat['reveal.core.open']);
    await expect(page.locator('[data-role="title"]')).toHaveText(cat['reveal.core.title.ready']);
    await expect(page.locator('[data-role="stage"] button')).toHaveText(cat['reveal.core.open']);
    await openGift(page);
    await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
    await expect(page.locator('[data-role="lead"]')).toHaveText(cat['reveal.core.lead.revealed']);
    await expect(page.locator('[data-role="status"]')).toHaveText(cat['reveal.core.announce'].replace('{name}', 'Léa'));
    const text = await page.evaluate(() => document.body.innerText);
    const attrs = await root(page).evaluate((el) => JSON.stringify({ ...(el as HTMLElement).dataset }));
    expect(text + attrs).not.toMatch(/reveal\.core\./);
    expect(text).not.toMatch(/\{(n|total|name)\}/);
  });
}

test('le script ne contient aucun texte affiché en dur', () => {
  const dir = TEMPLATE.replace(/templates\/reveal\/reveal\.html$/, 'static/js/reveal/core');
  for (const f of fs.readdirSync(dir)) {
    const code = fs.readFileSync(`${dir}/${f}`, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
    expect(code, f).not.toMatch(/[àâéèêëîïôùûçÀÉ]/);
    expect(code, f).not.toMatch(/textContent\s*=\s*['"`]/);
  }
});
