import { test, expect } from '@playwright/test';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

const dir = join(__dirname, '..', '..', '..', 'web', 'static', 'assets');
const required = ['reindeer', 'elf', 'tree', 'santa', 'mailbox', 'gifts', 'garland'];

const files = [
  ...readdirSync(dir).filter((f) => f.endsWith('.svg')).map((f) => join(dir, f)),
  ...readdirSync(join(dir, 'avatars')).map((f) => join(dir, 'avatars', f)),
];

test('les illustrations requises existent', () => {
  const present = readdirSync(dir);
  for (const n of required) expect(present).toContain(`${n}.svg`);
});

test('chaque SVG est bien forme, sans reference externe ni script', async ({ page }) => {
  for (const f of files) {
    const src = readFileSync(f, 'utf8');
    expect(src, f).toMatch(/^<svg xmlns="http:\/\/www\.w3\.org\/2000\/svg"/);
    expect(src.trim().endsWith('</svg>'), f).toBe(true);
    expect(src, f).not.toMatch(/<script|<style|<foreignObject|<image|data:|@import|on\w+\s*=|javascript:/i);
    expect(src, f).not.toMatch(/(https?:)?\/\/(?!www\.w3\.org\/2000\/svg)/);
    expect(src, f).not.toMatch(/(href|src)\s*=/i);
    expect(src, f).not.toMatch(/url\(\s*['"]?(?!#)/i);
    const parsed = await page.evaluate((s) => {
      const d = new DOMParser().parseFromString(s, 'image/svg+xml');
      return { err: d.getElementsByTagName('parsererror').length, root: d.documentElement.localName };
    }, src);
    expect(parsed, f).toEqual({ err: 0, root: 'svg' });
  }
});
