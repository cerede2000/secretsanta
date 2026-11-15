import { expect, type Page } from '@playwright/test';
import { expectNoHorizontalScroll } from './mobile';

/**
 * Cibles tactiles d'une page d'administration : tout ce qui se touche. Les cases
 * et boutons radio se touchent par leur etiquette (label) englobante.
 */
const TARGETS = [
  'button',
  'select',
  'summary',
  'textarea',
  '[role="button"]',
  '[role="menuitem"]',
  '[role="tab"]',
  '[role="switch"]',
  'input:not([type="hidden"]):not([type="checkbox"]):not([type="radio"])',
  'label:has(> input[type="checkbox"]), label:has(> input[type="radio"])',
  'a.btn, a.button, a[role="menuitem"], a[role="tab"]',
  '.admin-nav-item, .drw-tab, .dash-link, .ppl-name, .mb-entry',
  'a[class$="-back"]',
].join(', ');

/** Les elements tactiles visibles de moins de min px (hors etiquettes cachees au visuel). */
export async function smallTargets(page: Page, min = 44): Promise<string[]> {
  return page.evaluate(
    ({ selector, min }) => {
      const out: string[] = [];
      for (const el of document.querySelectorAll<HTMLElement>(selector)) {
        // Le contenu d'un <details> ferme garde une boite de mise en page mais ne se touche pas.
        const closed = el.closest('details:not([open])');
        if (closed && !closed.querySelector(':scope > summary')?.contains(el)) continue;
        const r = el.getBoundingClientRect();
        const cs = getComputedStyle(el);
        if (r.width <= 1 || r.height <= 1 || cs.visibility === 'hidden' || cs.display === 'none') continue;
        if (el.hasAttribute('hidden') || el.closest('[hidden]')) continue;
        if (r.width < min - 0.5 || r.height < min - 0.5) {
          const label = (el.textContent || el.getAttribute('aria-label') || '').replace(/\s+/g, ' ').trim().slice(0, 30);
          out.push(`${el.tagName.toLowerCase()}.${el.className.toString().split(' ')[0]} « ${label} » ${Math.round(r.width)}x${Math.round(r.height)}`);
        }
      }
      return out;
    },
    { selector: TARGETS, min },
  );
}

export async function expectTouchTargetsOk(page: Page): Promise<void> {
  expect(await smallTargets(page), 'cibles tactiles de moins de 44 px').toEqual([]);
}

/** Compte les boites natives (alert, confirm, prompt) ouvertes pendant le test ; les refuse. */
export function trackDialogs(page: Page): () => number {
  let n = 0;
  page.on('dialog', async (d) => {
    n += 1;
    await d.dismiss();
  });
  return () => n;
}

/** Ouvre la page et exige un 200. */
export async function open(page: Page, path: string): Promise<void> {
  const res = await page.goto(path);
  expect(res?.status(), `${path} doit repondre 200`).toBe(200);
}

/**
 * Controles communs a toute page d'administration (F10.AC24) : aucun defilement
 * horizontal, cibles de 44 px, aucun attribut style en ligne, aucune boite native.
 */
export async function expectCleanAdminPage(page: Page, dialogs?: () => number): Promise<void> {
  await expectNoHorizontalScroll(page);
  await expectTouchTargetsOk(page);
  expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
  if (dialogs) expect(dialogs(), 'boite native alert/confirm').toBe(0);
}
