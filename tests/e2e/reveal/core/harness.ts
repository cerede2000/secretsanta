import fs from 'node:fs';
import path from 'node:path';
import type { Page, Request } from '@playwright/test';

const WEB = path.resolve(__dirname, '../../../../web');
export const TEMPLATE = path.join(WEB, 'templates/reveal/reveal.html');
export const CATALOG_DIR = path.join(WEB, 'static/i18n/reveal/core');
export const JS_DIR = path.join(WEB, 'static/js/reveal');
export const CSS_DIR = path.join(WEB, 'static/css/reveal');

export const PAGE_PATH = '/__reveal_harness__';
export const TARGETS_PATH = '/__reveal_harness__/targets';
export const CONFIRM_PATH = '/__reveal_harness__/confirm';
export const CSP = "default-src 'self'; script-src 'self'; style-src 'self'";

export type Target = { id: string; name: string; hint?: string; budget?: string; deadline?: string };

export const ONE: Target[] = [
  { id: 't1', name: 'Léa', hint: 'Elle collectionne les mugs de Noël.', budget: '25 €', deadline: '24 décembre' },
];
export const TWO: Target[] = [
  ...ONE,
  { id: 't2', name: 'Noam', hint: 'Il adore les puzzles.', budget: '25 €', deadline: '24 décembre' },
];

export function catalog(lang: string): Record<string, string> {
  return JSON.parse(fs.readFileSync(path.join(CATALOG_DIR, `${lang}.json`), 'utf8'));
}

const esc = (s: string) =>
  s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Clés passées à `t` dans le gabarit, dans l'ordre. */
export function templateKeys(src: string = fs.readFileSync(TEMPLATE, 'utf8')): string[] {
  return [...src.matchAll(/\{\{t "([^"]+)"/g)].map((m) => m[1]);
}

/** Rend le vrai gabarit (sous-ensemble de html/template) avec le vrai catalogue. */
export function renderTemplate(lang: string, data: Record<string, string>): string {
  const cat = catalog(lang);
  let out = fs.readFileSync(TEMPLATE, 'utf8');
  out = out.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '');
  // Fragments des styles (enveloppe, cadeau) : les bancs d'essai les ajoutent eux-mêmes.
  out = out.replace(/\{\{template "[^"]+" \.\}\}\s*/g, '');
  out = out.replace(/\{\{if \.(\w+)\}\}([\s\S]*?)\{\{end\}\}/g, (_m, k, inner) => (data[k] ? inner : ''));
  out = out.replace(/\{\{define "layout\/content"\}\}/, '').replace(/\{\{end\}\}\s*$/, '');
  out = out.replace(/\{\{t "([^"]+)"((?: "[^"]*")*)\}\}/g, (_m, key, rest) => {
    let text = cat[key];
    if (text === undefined) throw new Error(`clé absente du catalogue ${lang} : ${key}`);
    const args = [...String(rest).matchAll(/"([^"]*)"/g)].map((m) => m[1]);
    for (let i = 0; i + 1 < args.length; i += 2) text = text.split(`{${args[i]}}`).join(args[i + 1]);
    return esc(text);
  });
  out = out.replace(/\{\{csrfToken\}\}/g, 'csrf-test-token');
  out = out.replace(/\{\{\.(\w+)\}\}/g, (_m, k) => esc(data[k] ?? ''));
  if (/\{\{/.test(out)) throw new Error('gabarit : action non rendue : ' + out.match(/\{\{[^}]*\}\}/)![0]);
  return out;
}

export type OpenOptions = {
  lang?: string;
  targets?: Target[];
  seed?: number;
  /** false : ne charge pas main.js (les tests pilotent createReveal eux-mêmes). */
  boot?: boolean;
  confirmStatus?: number;
  letterUrl?: string;
};

export type Harness = { confirms: Request[]; confirmBodies: string[]; violations: string[] };

/** Ouvre la page de révélation (gabarit réel, CSP stricte) avec une fausse API. */
export async function openReveal(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const lang = opts.lang ?? 'fr';
  const h: Harness = { confirms: [], confirmBodies: [], violations: [] };
  let body = renderTemplate(lang, {
    TargetsURL: TARGETS_PATH,
    ConfirmURL: CONFIRM_PATH,
    LetterURL: opts.letterUrl ?? '',
  });
  if (opts.seed !== undefined) body = body.replace('data-reveal ', `data-reveal data-seed="${opts.seed}" `);
  if (opts.boot === false) body = body.replace(/<script type="module"[^>]*><\/script>/, '');
  const html = `<!doctype html><html lang="${lang}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/static/css/base/base.css">
<link rel="stylesheet" href="/static/css/theme/theme.css"></head>
<body><main class="site-main">${body}</main></body></html>`;
  await page.route(`**${PAGE_PATH}`, (route) =>
    route.fulfill({ status: 200, contentType: 'text/html; charset=utf-8', headers: { 'Content-Security-Policy': CSP }, body: html }),
  );
  await page.route(`**${TARGETS_PATH}`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ targets: opts.targets ?? ONE }) }),
  );
  await page.route(`**${CONFIRM_PATH}`, (route) => {
    h.confirms.push(route.request());
    h.confirmBodies.push(route.request().postData() ?? '');
    return route.fulfill({ status: opts.confirmStatus ?? 204, body: '' });
  });
  await page.addInitScript(() => {
    (window as any).__violations = [];
    document.addEventListener('securitypolicyviolation', (e) => (window as any).__violations.push(e.violatedDirective + ' ' + e.blockedURI));
  });
  await page.goto(PAGE_PATH);
  await page.waitForLoadState('networkidle');
  return h;
}

export const root = (page: Page) => page.locator('[data-reveal]');

/** Pilote createReveal directement avec une fausse source (sans main.js). */
export async function startManual(
  page: Page,
  opts: { targets: Target[]; seed?: number; last?: string | null },
): Promise<void> {
  await page.evaluate(
    async ({ targets, seed, last }) => {
      const { createReveal } = await import('/static/js/reveal/core/reveal.js');
      const w = window as any;
      w.__confirmed = [];
      w.__remembered = [];
      const source = {
        targets: async () => targets,
        confirmReveal: async (t: { id: string }) => void w.__confirmed.push(t.id),
        lastStyle: () => last ?? null,
        rememberStyle: (s: string) => void w.__remembered.push(s),
      };
      w.__reveal = createReveal({ root: document.querySelector('[data-reveal]')!, source, seed });
      await w.__reveal.start();
    },
    { targets: opts.targets, seed: opts.seed, last: opts.last ?? null },
  );
}

/** Ouvre le cadeau courant avec le bouton équivalent. */
export async function openGift(page: Page): Promise<void> {
  await page.locator('[data-role="stage"] button.reveal-open').click();
}
