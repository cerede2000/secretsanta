import { expect, test } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { STATIC_DIR, openHarness, readStatic } from './harness';

test('la police manuscrite est servie localement, avec licence et repli systeme', async ({ page, request }) => {
  const fonts = path.join(STATIC_DIR, 'fonts');
  expect(fs.existsSync(path.join(fonts, 'mountains-of-christmas-latin-700-normal.woff2'))).toBe(true);
  const licences = fs.readdirSync(fonts).filter((f) => /^LICENSE/i.test(f));
  expect(licences.length, 'licence(s) versionnee(s)').toBeGreaterThanOrEqual(1);
  expect(readStatic('fonts/' + licences.find((l) => /Mountains/i.test(l))!)).toMatch(/Apache License|Open Font License/);

  const css = readStatic('css/theme/fonts.css');
  expect(css).toMatch(/font-family:\s*"Mountains of Christmas"/);
  expect(css).toMatch(/url\("\/static\/fonts\/mountains-of-christmas-latin-700-normal\.woff2"\)/);

  const res = await request.get('/static/fonts/mountains-of-christmas-latin-700-normal.woff2');
  expect(res.status()).toBe(200);
  const body = await res.body();
  expect(body.subarray(0, 4).toString('latin1')).toBe('wOF2');

  await openHarness(page);
  const info = await page.evaluate(async () => {
    await document.fonts.load('700 32px "Mountains of Christmas"');
    return {
      loaded: document.fonts.check('700 32px "Mountains of Christmas"'),
      stack: getComputedStyle(document.getElementById('title')!).fontFamily,
    };
  });
  expect(info.loaded).toBe(true);
  expect(info.stack).toContain('Mountains of Christmas');
  expect(info.stack).toMatch(/cursive|system-ui|sans-serif/);
});
