import fs from 'node:fs';
import path from 'node:path';
import type { Page, Request } from '@playwright/test';
import { CATALOG_DIR as CORE_CATALOG_DIR, CSP, ONE, TEMPLATE as CORE_TEMPLATE, type Target } from '../core/harness';

export { ONE, type Target };
const WEB = path.resolve(__dirname, '../../../../web');
export const SCRATCH_TEMPLATE = path.join(WEB, 'templates/reveal/scratch/scratch.html');
export const SCRATCH_CATALOG_DIR = path.join(WEB, 'static/i18n/reveal/scratch');
export const SCRATCH_JS = path.join(WEB, 'static/js/reveal/scratch/scratch.js');
export const SCRATCH_CSS = path.join(WEB, 'static/css/reveal/scratch/scratch.css');
export const PAGE_PATH = '/__scratch_harness__';
export const TARGETS_PATH = '/__scratch_harness__/targets';
export const CONFIRM_PATH = '/__scratch_harness__/confirm';

const read = (dir: string, lang: string): Record<string, string> =>
  JSON.parse(fs.readFileSync(path.join(dir, `${lang}.json`), 'utf8'));
export const scratchCatalog = (lang: string) => read(SCRATCH_CATALOG_DIR, lang);
export const coreCatalog = (lang: string) => read(CORE_CATALOG_DIR, lang);

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Clés passées à `t` dans le gabarit du scratch. */
export function scratchTemplateKeys(): string[] {
  return [...fs.readFileSync(SCRATCH_TEMPLATE, 'utf8').matchAll(/\{\{t "([^"]+)"/g)].map((m) => m[1]);
}

/** Rend un gabarit (sous-ensemble de html/template) avec les vrais catalogues. */
function render(file: string, lang: string, data: Record<string, string>, defineName: string): string {
  const cat = { ...coreCatalog(lang), ...scratchCatalog(lang) };
  let out = fs.readFileSync(file, 'utf8');
  out = out.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '');
  // Fragments des styles : les bancs d'essai les ajoutent eux-mêmes.
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
 * Ouvre la page de révélation (gabarit commun + gabarit du scratch, CSP stricte)
 * avec une fausse API. boot=false : main.js n'est pas chargé (le test pilote
 * createReveal avec une fausse source, voir startScratch).
 */
export async function openScratchPage(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const lang = opts.lang ?? 'fr';
  const h: Harness = { confirms: [], confirmBodies: [] };
  const data = { TargetsURL: TARGETS_PATH, ConfirmURL: CONFIRM_PATH, LetterURL: '' };
  let core = render(CORE_TEMPLATE, lang, data, 'layout/content');
  if (opts.boot === false) core = core.replace(/<script type="module"[^>]*><\/script>/, '');
  const gift = render(SCRATCH_TEMPLATE, lang, data, 'reveal/scratch/assets');
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
 * Force le style scratch en premier : le dernier style vu est « gift » et
 * l'aléa vaut presque 1, donc le tirage parmi (enveloppe, scratch) rend « scratch ».
 * Les confirmations passent par la vraie source HTTP (route simulée).
 */
export async function startScratch(page: Page, targets: Target[] = ONE): Promise<void> {
  await page.evaluate(async (list) => {
    const { createReveal } = await import('/static/js/reveal/core/reveal.js');
    const { httpSource } = await import('/static/js/reveal/core/http-source.js');
    const { styles } = await import('/static/js/reveal/core/registry.js');
    const root = document.querySelector('[data-reveal]') as HTMLElement;
    const real = httpSource(root);
    const source = { ...real, targets: async () => list, lastStyle: () => 'gift', rememberStyle: () => {} };
    const w = window as any;
    w.__reveal = createReveal({ root, source, styles, rand: () => 1 - 1e-9 });
    await w.__reveal.start();
  }, targets);
}

/** Page ouverte, style scratch lancé. */
export async function openScratch(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const h = await openScratchPage(page, { ...opts, boot: false });
  await startScratch(page, opts.targets ?? ONE);
  await page.waitForSelector('[data-reveal][data-style="scratch"]');
  return h;
}

export const art = (page: Page) => page.locator('.scratch-canvas');
export const nameEl = (page: Page) => page.locator('[data-role="name"]');

/** Fait glisser la souris en bandes horizontales couvrant la fraction `rows` (0..1) de la hauteur. */
export async function scratchRows(page: Page, from: number, to: number): Promise<void> {
  const box = (await art(page).boundingBox())!;
  const y0 = box.y + box.height * from;
  const y1 = box.y + box.height * to;
  for (let y = y0 + 8; y <= y1 - 8 + 0.1; y += 16) {
    await page.mouse.move(box.x + 6, y);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width - 6, y, { steps: 8 });
    await page.mouse.up();
  }
}
export const percent = (page: Page) => page.locator('.scratch').getAttribute('data-scratch').then((v) => Number(v));
