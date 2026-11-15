import { expect, test } from '@playwright/test';
import { contrast, openHarness } from './harness';

// Paires texte / fond (identifiants de la page d'essai).
const PAIRS: Array<[string, string, string?]> = [
  ['body', 'card'], ['secondary', 'card'], ['link', 'card'], ['title', 'body'],
  ['b-red', 'b-red'], ['b-green', 'b-green'], ['b-danger', 'b-danger'], ['b-gold', 'b-gold'],
  ['b-off', 'b-off'], ['c-ok', 'c-ok'], ['c-warn', 'c-warn'], ['c-error', 'c-error'],
  ['night-text', 'night'], ['night-title', 'night'], ['err', 'body'],
];

test('les contrastes de texte atteignent 4,5:1', async ({ page }) => {
  await openHarness(page);
  const colors = await page.evaluate(() => {
    const bgOf = (el: Element | null): string => {
      while (el) {
        const bg = getComputedStyle(el).backgroundColor;
        if (bg && bg !== 'rgba(0, 0, 0, 0)' && bg !== 'transparent') return bg;
        el = el.parentElement;
      }
      return 'rgb(255, 255, 255)';
    };
    const out: Record<string, { fg: string; bg: string }> = {};
    for (const el of Array.from(document.querySelectorAll('[id]'))) {
      out[el.id] = { fg: getComputedStyle(el).color, bg: bgOf(el) };
    }
    // Fond des zones de nuit : couleur de repli du degrade.
    out['night'] = { fg: out['night']?.fg, bg: getComputedStyle(document.getElementById('night')!).backgroundColor };
    return out;
  });
  for (const [fgId, bgId] of PAIRS) {
    const fg = colors[fgId].fg;
    const bg = fgId === bgId ? colors[bgId].bg : colors[bgId].bg;
    const ratio = contrast(fg, bg);
    expect(ratio, `${fgId} (${fg}) sur ${bgId} (${bg})`).toBeGreaterThanOrEqual(4.5);
  }
});
