import { test, expect } from '@playwright/test';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const root = join(__dirname, '..', '..', '..', 'web', 'static');
const modPath = join(root, 'js', 'avatars', 'avatars.js');

async function load() {
  return await import(pathToFileURL(modPath).href);
}

test('l avatar d un prenom est deterministe', async () => {
  const m = await load();
  for (const n of ['Alice', 'Zoé', 'Jean-Pierre', '']) {
    expect(m.avatarIndex(n)).toBe(m.avatarIndex(n));
  }
  expect(m.avatarIndex('  alice ')).toBe(m.avatarIndex('Alice'));
});

test('les prenoms se repartissent sur tous les avatars, qui existent', async () => {
  const m = await load();
  const seen = new Set<number>();
  for (let i = 0; i < 400; i++) seen.add(m.avatarIndex(`Prenom${i}`));
  expect(seen.size).toBe(m.AVATAR_COUNT);
  for (let i = 1; i <= m.AVATAR_COUNT; i++) {
    expect(existsSync(join(root, 'assets', 'avatars', `avatar-${i}.svg`))).toBe(true);
  }
  expect(existsSync(join(root, 'assets', 'avatars', `avatar-${m.AVATAR_COUNT + 1}.svg`))).toBe(false);
});
