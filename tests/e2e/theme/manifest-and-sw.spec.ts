import { expect, test } from '@playwright/test';
import { readStatic } from './harness';

test('le manifeste web est valide avec icones', async ({ request }) => {
  const res = await request.get('/static/manifest.webmanifest');
  expect(res.status()).toBe(200);
  const m = JSON.parse(await res.text());
  expect(m.name).toBeTruthy();
  expect(m.short_name).toBeTruthy();
  expect(m.start_url).toBe('/');
  expect(m.display).toBe('standalone');
  expect(m.theme_color).toMatch(/^#[0-9a-f]{6}$/i);
  expect(m.background_color).toMatch(/^#[0-9a-f]{6}$/i);
  const sizes = (m.icons as Array<{ sizes: string }>).map((i) => i.sizes);
  expect(sizes).toContain('192x192');
  expect(sizes).toContain('512x512');
  for (const icon of m.icons as Array<{ src: string; type: string }>) {
    expect(icon.src.startsWith('/'), 'icone locale').toBe(true);
    const r = await request.get(icon.src);
    expect(r.status(), icon.src).toBe(200);
    if (icon.type === 'image/png') {
      expect((await r.body()).subarray(1, 4).toString('latin1')).toBe('PNG');
    }
  }
});

test('le service worker ne met en cache aucune reponse du serveur', () => {
  const sw = readStatic('sw.js').replace(/\/\/.*$/gm, '');
  expect(sw, 'pas de gestionnaire fetch').not.toMatch(/addEventListener\(\s*['"]fetch['"]/);
  expect(sw, 'pas de respondWith').not.toMatch(/respondWith/);
  expect(sw, 'pas d\'ecriture en cache').not.toMatch(/caches\.(open|match)|\.(put|add|addAll)\(/);
  expect(sw).toMatch(/install/);
});
