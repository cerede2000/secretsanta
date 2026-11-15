import fs from 'node:fs';
import { expect, test } from '@playwright/test';
import { SCRATCH_JS, SCRATCH_TEMPLATE, openScratch, scratchCatalog, scratchTemplateKeys } from './harness';

const params = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

test('catalogues fr et en : mêmes clés, mêmes paramètres, préfixe reveal.scratch.', () => {
  const fr = scratchCatalog('fr');
  const en = scratchCatalog('en');
  expect(Object.keys(en).sort()).toEqual(Object.keys(fr).sort());
  for (const k of Object.keys(fr)) {
    expect(k.startsWith('reveal.scratch.'), k).toBe(true);
    expect(params(en[k])).toEqual(params(fr[k]));
    expect(fr[k].trim()).not.toBe('');
    expect(en[k].trim()).not.toBe('');
  }
});

test('le gabarit n\'écrit aucun texte brut et n\'utilise que des clés existantes', () => {
  const src = fs.readFileSync(SCRATCH_TEMPLATE, 'utf8');
  const fr = scratchCatalog('fr');
  const keys = scratchTemplateKeys();
  expect(keys.length).toBeGreaterThanOrEqual(3);
  for (const k of keys) expect(fr[k], k).toBeTruthy();
  const text = src.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '').replace(/\{\{[^}]*\}\}/g, '').replace(/<[^>]*>/g, '').trim();
  expect(text).toBe('');
  expect(src).not.toMatch(/\sstyle=|\son\w+=|data:/);
});

test('le script ne contient aucun texte affiché en dur', () => {
  const code = fs.readFileSync(SCRATCH_JS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
  expect(code).not.toMatch(/[àâéèêëîïôùûçÀÉ]/);
  expect(code).not.toMatch(/textContent\s*=\s*['"`]/);
});

for (const lang of ['fr', 'en']) {
  test(`rendu en ${lang} : textes fournis par attributs data, aucune clé brute`, async ({ page }) => {
    await openScratch(page, { lang });
    const cat = scratchCatalog(lang);
    const data = await page.locator('[data-reveal-scratch]').evaluate((el) => ({ ...(el as HTMLElement).dataset }));
    expect(data.tLabel).toBe(cat['reveal.scratch.label']);
    expect(data.tCard).toBe(cat['reveal.scratch.card']);
    expect(data.tScratch).toBe(cat['reveal.scratch.scratch']);
    expect(data.tAll).toBe(cat['reveal.scratch.all']);
    await expect(page.locator('.scratch-canvas')).toHaveAttribute('aria-label', cat['reveal.scratch.label']);
    await expect(page.locator('.scratch-title')).toHaveText(cat['reveal.scratch.card']);
    await expect(page.locator('.scratch-prompt')).toHaveText(cat['reveal.scratch.scratch']);
    await expect(page.locator('.scratch .reveal-open')).toHaveText(cat['reveal.scratch.all']);
    await expect(page.locator('.scratch-note')).toHaveText(cat['reveal.scratch.note']);
    const text = await page.evaluate(() => document.body.innerText);
    expect(text).not.toMatch(/reveal\.scratch\./);
  });
}
