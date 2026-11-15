import { expect, test } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { HARNESS_PATH, STATIC_DIR, openHarness, walk } from './harness';

const TEXT = /\.(css|js|html|webmanifest|svg|json|txt)$/;
// Domaine distant dans une reference de ressource (url(), src, href, import, @import).
const REMOTE = /(?:url\(\s*|src\s*=\s*|href\s*=\s*|@import\s+|import\s+(?:[^'"]*from\s*)?|fetch\()\s*["']?(?:https?:)?\/\/(?!www\.w3\.org\/)[a-z0-9.-]+/gi;

test('les sources du theme ne referencent aucun domaine distant', () => {
  const dirs = ['css/theme', 'js/theme', 'fonts'].map((d) => path.join(STATIC_DIR, d));
  const files = [...dirs.flatMap((d) => walk(d)), path.join(STATIC_DIR, 'manifest.webmanifest'), path.join(STATIC_DIR, 'sw.js')];
  expect(files.length).toBeGreaterThan(5);
  for (const f of files.filter((f) => TEXT.test(f))) {
    const src = fs.readFileSync(f, 'utf8');
    expect(src.match(REMOTE) ?? [], `reference distante dans ${f}`).toEqual([]);
    expect(src, `data: dans ${f}`).not.toMatch(/url\(\s*["']?data:/i);
    expect(src, `fonts.googleapis dans ${f}`).not.toMatch(/fonts\.(googleapis|gstatic)\.com/i);
  }
});

test('le navigateur ne fait aucune requete hors de l\'origine', async ({ page, baseURL }) => {
  const requests: string[] = [];
  await openHarness(page, requests);
  await page.evaluate(() => document.fonts.ready);
  const origin = new URL(baseURL ?? 'http://127.0.0.1:8080').origin;
  const foreign = requests.filter((u) => !u.startsWith(origin) && !u.startsWith('data:') && !u.startsWith('blob:'));
  expect(foreign).toEqual([]);
  expect(requests.some((u) => u.includes(HARNESS_PATH))).toBe(true);
});
