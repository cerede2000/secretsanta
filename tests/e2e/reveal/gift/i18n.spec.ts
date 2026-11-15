import fs from 'node:fs';
import { expect, test } from '@playwright/test';
import { GIFT_JS, GIFT_TEMPLATE, giftCatalog, giftTemplateKeys, openGift } from './harness';

const params = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

test('catalogues fr et en : mêmes clés, mêmes paramètres, préfixe reveal.gift.', () => {
  const fr = giftCatalog('fr');
  const en = giftCatalog('en');
  expect(Object.keys(en).sort()).toEqual(Object.keys(fr).sort());
  for (const k of Object.keys(fr)) {
    expect(k.startsWith('reveal.gift.'), k).toBe(true);
    expect(params(en[k])).toEqual(params(fr[k]));
    expect(fr[k].trim()).not.toBe('');
    expect(en[k].trim()).not.toBe('');
  }
});

test('le gabarit n\'écrit aucun texte brut et n\'utilise que des clés existantes', () => {
  const src = fs.readFileSync(GIFT_TEMPLATE, 'utf8');
  const fr = giftCatalog('fr');
  const keys = giftTemplateKeys();
  expect(keys.length).toBeGreaterThanOrEqual(3);
  for (const k of keys) expect(fr[k], k).toBeTruthy();
  const text = src.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '').replace(/\{\{[^}]*\}\}/g, '').replace(/<[^>]*>/g, '').trim();
  expect(text).toBe('');
  expect(src).not.toMatch(/\sstyle=|\son\w+=|data:/);
});

test('le script ne contient aucun texte affiché en dur', () => {
  const code = fs.readFileSync(GIFT_JS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
  expect(code).not.toMatch(/[àâéèêëîïôùûçÀÉ]/);
  expect(code).not.toMatch(/textContent\s*=\s*['"`]/);
});

for (const lang of ['fr', 'en']) {
  test(`rendu en ${lang} : textes fournis par attributs data, aucune clé brute`, async ({ page }) => {
    await openGift(page, { lang });
    const cat = giftCatalog(lang);
    const data = await page.locator('[data-reveal-gift]').evaluate((el) => ({ ...(el as HTMLElement).dataset }));
    expect(data.tLabel).toBe(cat['reveal.gift.label']);
    expect(data.tHold).toBe(cat['reveal.gift.hold']);
    expect(data.tHolding).toBe(cat['reveal.gift.holding']);
    await expect(page.locator('.gift-art')).toHaveAttribute('aria-label', cat['reveal.gift.label']);
    await expect(page.locator('.gift-hint')).toHaveText(cat['reveal.gift.hold']);
    await expect(page.locator('.gift-note')).toHaveText(cat['reveal.gift.note']);
    const text = await page.evaluate(() => document.body.innerText);
    expect(text).not.toMatch(/reveal\.gift\./);
  });
}
