import fs from 'node:fs';
import { expect, test } from '@playwright/test';
import { ENVELOPE_JS, ENVELOPE_TEMPLATE, envelopeCatalog, envelopeTemplateKeys, openEnvelope } from './harness';

const params = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

test('catalogues fr et en : mêmes clés, mêmes paramètres, préfixe reveal.envelope.', () => {
  const fr = envelopeCatalog('fr');
  const en = envelopeCatalog('en');
  expect(Object.keys(en).sort()).toEqual(Object.keys(fr).sort());
  expect(Object.keys(fr).length).toBeGreaterThanOrEqual(2);
  for (const k of Object.keys(fr)) {
    expect(k.startsWith('reveal.envelope.'), k).toBe(true);
    expect(params(en[k])).toEqual(params(fr[k]));
    expect(fr[k].trim()).not.toBe('');
    expect(en[k].trim()).not.toBe('');
  }
});

test('le français tutoie : jamais de vouvoiement dans le catalogue', () => {
  for (const [k, v] of Object.entries(envelopeCatalog('fr'))) {
    expect(v, k).not.toMatch(/\b(vous|votre|vos|touchez|appuyez|brisez)\b/i);
  }
  expect(envelopeCatalog('fr')['reveal.envelope.hint']).toMatch(/\bTouche\b/);
});

test('le gabarit n\'écrit aucun texte brut et n\'utilise que des clés existantes', () => {
  const src = fs.readFileSync(ENVELOPE_TEMPLATE, 'utf8');
  const fr = envelopeCatalog('fr');
  const keys = envelopeTemplateKeys();
  expect(keys.length).toBeGreaterThanOrEqual(2);
  for (const k of keys) expect(fr[k], k).toBeTruthy();
  // Chaque clé du catalogue est utilisée par le gabarit.
  for (const k of Object.keys(fr)) expect(keys, k).toContain(k);
  const text = src.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '').replace(/\{\{[^}]*\}\}/g, '').replace(/<[^>]*>/g, '').trim();
  expect(text).toBe('');
  expect(src).not.toMatch(/\sstyle=|\son\w+=|data:/);
});

test('le script ne contient aucun texte affiché en dur', () => {
  const code = fs.readFileSync(ENVELOPE_JS, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
  expect(code).not.toMatch(/[àâéèêëîïôùûçÀÉ]/);
  expect(code).not.toMatch(/textContent\s*=\s*['"`]/);
  expect(code).not.toMatch(/setAttribute\(\s*'aria-label'\s*,\s*['"`]/);
});

for (const lang of ['fr', 'en']) {
  test(`rendu en ${lang} : textes fournis par attributs data, aucune clé brute`, async ({ page }) => {
    await openEnvelope(page, { lang });
    const cat = envelopeCatalog(lang);
    const data = await page.locator('[data-envelope-labels]').evaluate((el) => ({ ...(el as HTMLElement).dataset }));
    expect(data.tSeal).toBe(cat['reveal.envelope.seal']);
    expect(data.tEnvelopeHint).toBe(cat['reveal.envelope.hint']);
    await expect(page.locator('button[data-role="seal"]')).toHaveAttribute('aria-label', cat['reveal.envelope.seal']);
    await expect(page.locator('.envelope-hint')).toHaveText(cat['reveal.envelope.hint']);
    const text = await page.evaluate(() => document.body.innerText);
    expect(text).not.toMatch(/reveal\.envelope\./);
    expect(text).not.toMatch(/reveal\.[a-z]+\.[a-z]/);
  });
}
