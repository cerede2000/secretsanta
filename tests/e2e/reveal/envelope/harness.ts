import fs from 'node:fs';
import path from 'node:path';
import type { Page, Request } from '@playwright/test';
import { CATALOG_DIR as CORE_CATALOG_DIR, CSP, ONE, TWO, TEMPLATE as CORE_TEMPLATE, type Target } from '../core/harness';

export { ONE, TWO, type Target };
const WEB = path.resolve(__dirname, '../../../../web');
export const ENVELOPE_TEMPLATE = path.join(WEB, 'templates/reveal/envelope/envelope.html');
export const ENVELOPE_CATALOG_DIR = path.join(WEB, 'static/i18n/reveal/envelope');
export const ENVELOPE_JS = path.join(WEB, 'static/js/reveal/envelope/envelope.js');
export const ENVELOPE_CSS = path.join(WEB, 'static/css/reveal/envelope/envelope.css');
export const PAGE_PATH = '/__envelope_harness__';
export const TARGETS_PATH = '/__envelope_harness__/targets';
export const CONFIRM_PATH = '/__envelope_harness__/confirm';

const read = (dir: string, lang: string): Record<string, string> =>
  JSON.parse(fs.readFileSync(path.join(dir, `${lang}.json`), 'utf8'));
export const envelopeCatalog = (lang: string) => read(ENVELOPE_CATALOG_DIR, lang);
export const coreCatalog = (lang: string) => read(CORE_CATALOG_DIR, lang);

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

/** Clés passées à `t` dans le gabarit de l'enveloppe. */
export function envelopeTemplateKeys(): string[] {
  return [...fs.readFileSync(ENVELOPE_TEMPLATE, 'utf8').matchAll(/\{\{t "([^"]+)"/g)].map((m) => m[1]);
}

/** Rend un gabarit (sous-ensemble de html/template) avec les vrais catalogues. */
function render(file: string, lang: string, data: Record<string, string>, defineName: string): string {
  const cat = { ...coreCatalog(lang), ...envelopeCatalog(lang) };
  let out = fs.readFileSync(file, 'utf8');
  out = out.replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, '');
  // Fragments des styles (enveloppe, enveloppe) : les bancs d'essai les ajoutent eux-mêmes.
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
 * Ouvre la page de révélation (gabarit commun + gabarit de l'enveloppe, CSP stricte)
 * avec une fausse API. boot=false : main.js n'est pas chargé (le test pilote
 * createReveal avec une fausse source, voir startEnvelope).
 */
export async function openEnvelopePage(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const lang = opts.lang ?? 'fr';
  const h: Harness = { confirms: [], confirmBodies: [] };
  const data = { TargetsURL: TARGETS_PATH, ConfirmURL: CONFIRM_PATH, LetterURL: '' };
  let core = render(CORE_TEMPLATE, lang, data, 'layout/content');
  if (opts.boot === false) core = core.replace(/<script type="module"[^>]*><\/script>/, '');
  const fragment = render(ENVELOPE_TEMPLATE, lang, data, 'reveal/envelope/assets');
  const html = `<!doctype html><html lang="${lang}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/static/css/base/base.css">
<link rel="stylesheet" href="/static/css/theme/theme.css"></head>
<body><main class="site-main">${core}${fragment}</main></body></html>`;
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

/**
 * Force le style enveloppe : le dernier style vu est « gift » et l'aléa vaut 0,
 * donc le tirage parmi (enveloppe, carte) rend « envelope ».
 * Les confirmations passent par la vraie source HTTP (route simulée).
 */
export async function startEnvelope(page: Page, targets: Target[] = ONE): Promise<void> {
  await page.evaluate(async (list) => {
    const { createReveal } = await import('/static/js/reveal/core/reveal.js');
    const { httpSource } = await import('/static/js/reveal/core/http-source.js');
    const { styles } = await import('/static/js/reveal/core/registry.js');
    const root = document.querySelector('[data-reveal]') as HTMLElement;
    const real = httpSource(root);
    const source = { ...real, targets: async () => list, lastStyle: () => 'gift', rememberStyle: () => {} };
    const w = window as any;
    w.__reveal = createReveal({ root, source, styles, rand: () => 0 });
    await w.__reveal.start();
  }, targets);
}

/** Page ouverte, style enveloppe lancé. */
export async function openEnvelope(page: Page, opts: OpenOptions = {}): Promise<Harness> {
  const h = await openEnvelopePage(page, { ...opts, boot: false });
  await startEnvelope(page, opts.targets ?? ONE);
  await page.waitForSelector('[data-reveal][data-style="envelope"]');
  return h;
}

export const art = (page: Page) => page.locator('.envelope-seal-btn');
export const seal = (page: Page) => page.locator('[data-role="stage"] button[data-role="seal"]');
export const equivalent = (page: Page) => page.locator('[data-role="stage"] button.reveal-open');
export const nameEl = (page: Page) => page.locator('[data-role="name"]');
