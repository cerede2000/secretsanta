import fs from 'node:fs';
import path from 'node:path';
import type { Page, Request } from '@playwright/test';
import { CATALOG_DIR as CORE_CATALOG_DIR, CSP, ONE, TEMPLATE as CORE_TEMPLATE, type Target } from '../core/harness';

export { ONE, type Target };
const WEB = path.resolve(__dirname, '../../../../web');
export const GIFT_TEMPLATE = path.join(WEB, 'templates/reveal/gift/gift.html');
export const GIFT_CATALOG_DIR = path.join(WEB, 'static/i18n/reveal/gift');
export const GIFT_JS = path.join(WEB, 'static/js/reveal/gift/gift.js');
export const GIFT_CSS = path.join(WEB, 'static/css/reveal/gift/gift.css');
export const PAGE_PATH = '/__gift_harness__';
export const TARGETS_PATH = '/__gift_harness__/targets';
export const CONFIRM_PATH = '/__gift_harness__/confirm';

const read = (dir: string, lang: string): Record<string, string> =>
  JSON.parse(fs.readFileSync(path.join(dir, `${lang}.json`), 'utf8'));
export const giftCatalog = (lang: string) => read(GIFT_CATALOG_DIR, lang);
export const coreCatalog = (lang: string) => read(CORE_CATALOG_DIR, lang);

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Clés passées à `t` dans le gabarit du cadeau. */
export function giftTemplateKeys(): string[] {
  return [...fs.readFileSync(GIFT_TEMPLATE, 'utf8').matchAll(/\{\{t "([^"]+)"/g)].map((m) => m[1]);
}

/** Rend un gabarit (sous-ensemble de html/template) avec les vrais catalogues. */
function render(file: string, lang: string, data: Record<string, string>, defineName: string): string {
  const cat = { ...coreCatalog(lang), ...giftCatalog(lang) };
  let out = fs.readFileSync(file, 'utf8');
  out = out.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '');
  // Fragments des styles (enveloppe, cadeau) : les bancs d'essai les ajoutent eux-mêmes.
  out = out.replace(/\{\{template "[^"]+" \.\}\}\s*/g, '');
  out = out.replace(/\{\{if \.(\w+)\}\}([\s\S]*?)\{\{end\}\}/g, (_m, k, inner) => (data[k] ? inner : ''));
  out = out.replace(new RegExp(`\\{\\{define "${defineName}"\\}\\}`), '').replace(/\{\{end\}\}\s*$/, '');
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

export type OpenOptions = { lang?: string; targets?: Target[]; boot?: boolean; confirmStatus?: number };
export type Harness = { confirms: Request[]; confirmBodies: string[] };

/**
 * Ouvre la page de révélation (gabarit commun + gabarit du cadeau, CSP stricte)
 * avec une fausse API. boot=false : main.js n'est pas chargé (le test pilote
 * createReveal avec une fausse source, voir startGift).
 */
export async function openGiftPage(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const lang = opts.lang ?? 'fr';
  const h: Harness = { confirms: [], confirmBodies: [] };
  const data = { TargetsURL: TARGETS_PATH, ConfirmURL: CONFIRM_PATH, LetterURL: '' };
  let core = render(CORE_TEMPLATE, lang, data, 'layout/content');
  if (opts.boot === false) core = core.replace(/<script type="module"[^>]*><\/script>/, '');
  const gift = render(GIFT_TEMPLATE, lang, data, 'reveal/gift/assets');
  const html = `<!doctype html><html lang="${lang}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/static/css/base/base.css">
<link rel="stylesheet" href="/static/css/theme/theme.css"></head>
<body><main class="site-main">${core}${gift}</main></body></html>`;
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
  await page.goto(PAGE_PATH);
  await page.waitForLoadState('networkidle');
  return h;
}

/**
 * Force le style cadeau en premier : le dernier style vu est « scratch » et
 * l'aléa vaut 0, donc le tirage parmi (cadeau, enveloppe) rend « gift ».
 * Les confirmations passent par la vraie source HTTP (route simulée).
 */
export async function startGift(page: Page, targets: Target[] = ONE): Promise<void> {
  await page.evaluate(async (list) => {
    const { createReveal } = await import('/static/js/reveal/core/reveal.js');
    const { httpSource } = await import('/static/js/reveal/core/http-source.js');
    const { styles } = await import('/static/js/reveal/core/registry.js');
    const root = document.querySelector('[data-reveal]') as HTMLElement;
    const real = httpSource(root);
    const source = { ...real, targets: async () => list, lastStyle: () => 'scratch', rememberStyle: () => {} };
    const w = window as any;
    w.__reveal = createReveal({ root, source, styles, rand: () => 0 });
    await w.__reveal.start();
  }, targets);
}

/** Page ouverte, style cadeau lancé. */
export async function openGift(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const h = await openGiftPage(page, { ...opts, boot: false });
  await startGift(page, opts.targets ?? ONE);
  await page.waitForSelector('[data-reveal][data-style="gift"]');
  return h;
}

export const art = (page: Page) => page.locator('.gift-art');
export const nameEl = (page: Page) => page.locator('[data-role="name"]');
