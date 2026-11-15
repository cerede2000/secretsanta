import fs from 'node:fs';
import path from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared';
import { ONE, TWO, openReveal, root, type Target } from '../core/harness';

const I18N = path.resolve(__dirname, '../../../../web/static/i18n/reveal/envelope');
const cat = (lang: string): Record<string, string> => JSON.parse(fs.readFileSync(path.join(I18N, `${lang}.json`), 'utf8'));

/**
 * Monte la page de révélation réelle (gabarit de cœur, CSP stricte), ajoute le
 * fragment du style enveloppe (feuille de style, textes en data-t-*, module)
 * et force le tirage sur « envelope » (dernier style vu : gift, graine 0).
 */
async function boot(page: Page, targets: Target[], extra: { last?: string; lang?: string } = {}) {
  const lang = extra.lang ?? 'fr';
  const h = await openReveal(page, { targets, lang, boot: false });
  const c = cat(lang);
  await page.evaluate(
    async ({ targets, last, c }) => {
      const w = window as any;
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = '/static/css/reveal/envelope/envelope.css';
      document.head.appendChild(link);
      const holder = document.createElement('div');
      holder.hidden = true;
      holder.setAttribute('data-envelope-labels', '');
      holder.setAttribute('data-t-seal', c['reveal.envelope.seal']);
      holder.setAttribute('data-t-envelope-hint', c['reveal.envelope.hint']);
      document.body.appendChild(holder);
      await import('/static/js/reveal/envelope/envelope.js');
      const { createReveal } = await import('/static/js/reveal/core/reveal.js');
      const { styles } = await import('/static/js/reveal/core/registry.js');
      w.__confirmed = [];
      const source = {
        targets: async () => targets,
        confirmReveal: async (t: { id: string }) => void w.__confirmed.push(t.id),
        lastStyle: () => last,
        rememberStyle: () => {},
      };
      w.__reveal = createReveal({ root: document.querySelector('[data-reveal]')!, source, styles, rand: () => 0 });
      await w.__reveal.start();
    },
    { targets, last: extra.last ?? 'gift', c },
  );
  return h;
}

const confirmed = (page: Page) => page.evaluate(() => (window as any).__confirmed as string[]);
const seal = (page: Page) => page.locator('[data-role="stage"] button[data-role="seal"]');
const equivalent = (page: Page) => page.locator('[data-role="stage"] button.reveal-open');

test('toucher le sceau le brise, la lettre sort et la révélation est confirmée une seule fois', async ({ page }) => {
  await boot(page, ONE);
  await expect(root(page)).toHaveAttribute('data-style', 'envelope');
  await expect(root(page)).toHaveAttribute('data-phase', 'ready');
  await expect(seal(page)).toHaveAccessibleName('Briser le sceau');

  await seal(page).tap();
  await expect(page.locator('.envelope')).toHaveClass(/is-breaking/);
  await expect(root(page)).toHaveAttribute('data-phase', 'progress');
  await expect(page.locator('.envelope-seal-l')).toBeAttached();
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
  await expect.poll(() => confirmed(page)).toEqual(['t1']);
  // Un second toucher (ou un double clic) ne renvoie rien.
  await page.waitForTimeout(400);
  expect(await confirmed(page)).toEqual(['t1']);
});

test('des touchers répétés sur le sceau et le bouton ne confirment qu\'une fois', async ({ page }) => {
  await boot(page, ONE);
  await seal(page).dispatchEvent('click');
  await seal(page).dispatchEvent('click');
  await equivalent(page).dispatchEvent('click');
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  await page.waitForTimeout(500);
  expect(await confirmed(page)).toEqual(['t1']);
});

test('le bouton équivalent, au clavier, produit le même résultat', async ({ page }) => {
  await boot(page, ONE);
  await equivalent(page).focus();
  await page.keyboard.press('Enter');
  await expect(page.locator('.envelope')).toHaveClass(/is-breaking/);
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
  expect(await confirmed(page)).toEqual(['t1']);
});

test('le sceau lui-même s\'active au clavier (Espace)', async ({ page }) => {
  await boot(page, ONE);
  await seal(page).focus();
  await page.keyboard.press('Space');
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  expect(await confirmed(page)).toEqual(['t1']);
});

test('mobile : pas de défilement horizontal, cibles tactiles >= 44 px, sans violation de CSP', async ({ page }) => {
  await boot(page, TWO);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  await seal(page).tap();
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  await expectNoHorizontalScroll(page);
  expect(await page.evaluate(() => (window as any).__violations)).toEqual([]);
});

test('textes anglais : le sceau et l\'indication viennent du catalogue', async ({ page }) => {
  await boot(page, ONE, { lang: 'en' });
  await expect(seal(page)).toHaveAccessibleName('Break the seal');
  await expect(page.locator('.envelope-hint')).toHaveText('Tap the red seal to open it');
});

test('animations par transform et opacity uniquement', async ({ page }) => {
  await boot(page, ONE);
  const props = await page.evaluate(() => {
    const out = new Set<string>();
    for (const sel of ['.envelope-letter', '.envelope-flap', '.envelope-seal-l', '.envelope-seal-r', '.envelope-star']) {
      const cs = getComputedStyle(document.querySelector(sel)!);
      cs.transitionProperty.split(',').forEach((p) => out.add(p.trim()));
    }
    return [...out];
  });
  for (const p of props) expect(['transform', 'opacity']).toContain(p);
});

test('prefers-reduced-motion : repli en fondu, sans enveloppe animée', async ({ browser }) => {
  const ctx = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 360, height: 640 }, hasTouch: true });
  const page = await ctx.newPage();
  await boot(page, ONE);
  await expect(root(page)).toHaveAttribute('data-motion', 'reduced');
  await expect(page.locator('.envelope')).toHaveCount(0);
  await equivalent(page).click();
  await expect(root(page)).toHaveAttribute('data-phase', 'revealed');
  await expect(page.locator('[data-role="name"]')).toHaveText('Léa');
  expect(await confirmed(page)).toEqual(['t1']);
  await ctx.close();
});
