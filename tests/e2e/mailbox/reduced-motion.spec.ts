import { expect, test } from '@playwright/test';
import { openMailbox, root } from './harness';

const flag = '.mb-night .mb-flag';
const letter = '.mb-night .mb-letter';

test('prefers-reduced-motion : aucune animation de la boîte aux lettres', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openMailbox(page, { State: 'sent' });
  await expect(root(page)).toHaveAttribute('data-motion', 'reduced');
  const names = await page.evaluate(
    ([f, l]) => ({
      flag: getComputedStyle(document.querySelector(f)!).animationName,
      letter: getComputedStyle(document.querySelector(l)!).animationName,
      animated: document.querySelector('.mb-art')!.classList.contains('is-animated'),
      running: document.getAnimations().length,
    }),
    [flag, letter],
  );
  expect(names.flag).toBe('none');
  expect(names.letter).toBe('none');
  expect(names.animated).toBe(false);
  expect(names.running).toBe(0);
});

test('sans prefers-reduced-motion : le drapeau se balance et la lettre tombe', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openMailbox(page, { State: 'sent' });
  await expect(root(page)).toHaveAttribute('data-motion', 'full');
  const names = await page.evaluate(
    ([f, l]) => ({
      flag: getComputedStyle(document.querySelector(f)!).animationName,
      letter: getComputedStyle(document.querySelector(l)!).animationName,
    }),
    [flag, letter],
  );
  expect(names.flag).toBe('mb-bob');
  expect(names.letter).toBe('mb-drop');
});

test('le changement de préférence coupe l\'animation sans recharger', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await openMailbox(page, { State: 'sent' });
  await expect(root(page)).toHaveAttribute('data-motion', 'full');
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await expect(root(page)).toHaveAttribute('data-motion', 'reduced');
  expect(await page.evaluate((f) => getComputedStyle(document.querySelector(f)!).animationName, flag)).toBe('none');
});

test('le drapeau ne bouge que pour une lettre envoyée', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  for (const state of ['empty', 'draft', 'read'] as const) {
    await openMailbox(page, { State: state });
    expect(await page.evaluate((f) => getComputedStyle(document.querySelector(f)!).animationName, flag)).toBe('none');
    await page.unroute('**/__mailbox_harness__');
  }
});
