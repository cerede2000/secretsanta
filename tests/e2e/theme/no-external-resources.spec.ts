import { expect, test } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { HARNESS_PATH, openHarness, walk } from './harness';
import { ADMIN_PATH, HOME_PATH } from '../foundation/paths';

const WEB_DIR = path.resolve(__dirname, '../../../web');
const TEXT = /\.(css|js|mjs|html|webmanifest|svg|json|txt|xml)$/i;
const HOST = String.raw`[a-z0-9][a-z0-9.-]*\.[a-z]{2,}|localhost|\d{1,3}(?:\.\d{1,3}){3}`;
// Reference a une URL distante (http(s):// ou //hote) apres src/href/srcset/action/poster/data-*, url(), @import, import(), fetch(), XHR, WebSocket, EventSource...
const URL_START = String.raw`(?:https?:)?//(?:${HOST})`;
const PATTERNS: Array<[string, RegExp]> = [
  ['attribut src/href/srcset/action/poster/content', new RegExp(String.raw`\b(?:src|href|srcset|action|poster|data-[a-z-]*|xlink:href|content)\s*=\s*["']?\s*(?:[^"'>]*[\s,])?${URL_START}`, 'gi')],
  ['url()', new RegExp(String.raw`url\(\s*["']?\s*${URL_START}`, 'gi')],
  ['@import', new RegExp(String.raw`@import\s+(?:url\(\s*)?["']?\s*${URL_START}`, 'gi')],
  ['import statique ou dynamique', new RegExp(String.raw`\bimport\s*(?:\(|[^;'"]*?from)?\s*["']${URL_START}`, 'gi')],
  ['fetch/XHR/WebSocket/EventSource/sendBeacon', new RegExp(String.raw`(?:fetch|\.open|WebSocket|EventSource|sendBeacon|importScripts)\s*\(\s*(?:["'][A-Za-z]+["']\s*,\s*)?["'\x60]${URL_START}`, 'gi')],
  ['chaine d\'URL distante dans un script', new RegExp(String.raw`["'\x60](?:https?:)?//(?:${HOST})[/"'\x60?#]`, 'gi')],
  ['hotes de polices ou de CDN', /(?:fonts\.(?:googleapis|gstatic)\.com|cdnjs|jsdelivr|unpkg\.com|cloudflare\.com|bootstrapcdn|googleapis\.com|gstatic\.com|typekit|fontawesome|use\.fontawesome|ajax\.googleapis)/gi],
  ['data: dans url()', /url\(\s*["']?data:/gi],
];

// Liste d'exceptions : mentions qui ne declenchent aucune requete. Chaque entree vise un
// type de fichier precis et retire du texte analyse SEULEMENT la mention autorisee
// (le reste de la ligne reste analyse).
const ALLOW: Array<{ file: RegExp; strip: RegExp; why: string }> = [
  // Espaces de noms XML/SVG : des identifiants, jamais charges.
  { file: /\.svg$/, strip: /\bxmlns(?::[a-z]+)?\s*=\s*["']http:\/\/www\.w3\.org\/(?:2000\/svg|1999\/xlink)["']/gi, why: 'espace de noms SVG/XLink dans un .svg' },
  // Texte d'une licence de police (OFL) : prose et URL de reference, jamais charge.
  { file: /^static\/fonts\/[^/]*(?:LICENSE|OFL|licen[cs]e)[^/]*\.txt$/i, strip: /.*/g, why: 'texte de licence de police' },
];

/** Liste les violations d'un fichier, ligne par ligne, apres retrait des mentions autorisees. */
function violations(rel: string, src: string): string[] {
  const out: string[] = [];
  const lines = src.split('\n');
  lines.forEach((line, i) => {
    let l = line;
    for (const a of ALLOW) if (a.file.test(rel)) l = l.replace(a.strip, '');
    for (const [label, re] of PATTERNS) {
      re.lastIndex = 0;
      if (!re.test(l)) continue;
      out.push(`${rel}:${i + 1} [${label}] ${line.trim().slice(0, 160)}`);
    }
  });
  return out;
}

test('aucune source de web/templates et web/static ne reference un domaine distant', () => {
  const files = [...walk(path.join(WEB_DIR, "templates")), ...walk(path.join(WEB_DIR, "static"))];
  const texts = files.filter((f) => TEXT.test(f));
  // Garde-fou : on doit vraiment parcourir les gabarits et les ressources.
  expect(texts.filter((f) => f.includes(`${path.sep}templates${path.sep}`)).length).toBeGreaterThanOrEqual(2);
  for (const ext of ['css', 'js', 'svg', 'json', 'webmanifest', 'html']) {
    expect(texts.some((f) => f.endsWith(`.${ext}`)), `aucun fichier .${ext} parcouru`).toBe(true);
  }
  // Les fichiers non texte inconnus ne doivent pas echapper a l'analyse.
  const unknown = files.filter((f) => !TEXT.test(f) && !/\.(woff2?|png|jpe?g|webp|gif|ico)$/i.test(f));
  expect(unknown, 'type de fichier non analyse').toEqual([]);
  const found: string[] = [];
  for (const f of texts) {
    const rel = path.relative(WEB_DIR, f).split(path.sep).join('/');
    found.push(...violations(rel, fs.readFileSync(f, 'utf8')));
  }
  expect(found).toEqual([]);
});

test('le detecteur voit bien les violations injectees', () => {
  const bad: Array<[string, string]> = [
    ['templates/layout/x.html', '<script src="https://cdn.example.com/a.js"></script>'],
    ['templates/layout/x.html', '<link rel="stylesheet" href="//fonts.googleapis.com/css?family=A">'],
    ['templates/layout/x.html', '<img srcset="a.png 1x, https://img.example.org/a.png 2x">'],
    ['static/css/x.css', 'body{background:url(https://example.com/a.png)}'],
    ['static/css/x.css', '@import "https://example.com/a.css";'],
    ['static/css/x.css', '@import url(//example.com/a.css);'],
    ['static/js/x.js', "import('https://example.com/a.js')"],
    ['static/js/x.js', "import x from 'https://example.com/a.js'"],
    ['static/js/x.js', "fetch('https://api.example.com/x')"],
    ['static/js/x.js', "xhr.open('GET', 'https://api.example.com/x')"],
    ['static/js/x.js', "const u = '//tracker.example.com/p'"],
    ['static/assets/x.svg', '<image href="https://example.com/a.png"/>'],
    ['static/manifest.webmanifest', '{"src": "https://example.com/i.png"}'],
    ['static/css/x.css', 'a{background:url(data:image/png;base64,AAAA)}'],
  ];
  for (const [rel, src] of bad) expect(violations(rel, src), `${rel}: ${src}`).not.toEqual([]);
  // Les exceptions sont etroites : l'espace de noms SVG passe seulement dans un .svg.
  expect(violations('static/assets/ok.svg', '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>')).toEqual([]);
  expect(violations('static/css/ok.css', 'a{background:url(http://www.w3.org/2000/svg)}')).not.toEqual([]);
  expect(violations('static/assets/x.svg', '<svg xmlns="http://www.w3.org/2000/svg"><image href="https://example.com/a.png"/></svg>')).not.toEqual([]);
  // Des references locales ne sont pas des violations.
  expect(violations('templates/layout/x.html', '<script type="module" src="/static/js/theme/snow.js"></script>')).toEqual([]);
});

function sameOrigin(u: string, origin: string): boolean {
  return u.startsWith(`${origin}/`) || u === origin || u.startsWith('data:') || u.startsWith('blob:') || u === 'about:blank';
}

test('le navigateur ne fait aucune requete hors de l\'origine sur la page d\'essai du theme', async ({ page, baseURL }) => {
  const requests: string[] = [];
  await openHarness(page, requests);
  await page.evaluate(() => document.fonts.ready);
  const origin = new URL(baseURL ?? 'http://127.0.0.1:8080').origin;
  expect(requests.filter((u) => !sameOrigin(u, origin))).toEqual([]);
  expect(requests.some((u) => u.includes(HARNESS_PATH))).toBe(true);
});

for (const [name, route] of [['accueil', HOME_PATH], ['admin', ADMIN_PATH]] as const) {
  test(`page reelle ${name} : toutes les requetes vont a l'origine de l'application, sans erreur de ressource bloquee`, async ({ page, baseURL }) => {
    const origin = new URL(baseURL ?? 'http://127.0.0.1:8080').origin;
    const requests: string[] = [];
    const failures: string[] = [];
    const consoleErrors: string[] = [];
    page.on('request', (r) => requests.push(r.url()));
    page.on('requestfailed', (r) => failures.push(`${r.url()} ${r.failure()?.errorText ?? ''}`));
    page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
    page.on('pageerror', (e) => consoleErrors.push(e.message));
    const resp = await page.goto(route);
    expect(resp?.status()).toBe(200);
    await page.waitForLoadState('networkidle');
    await page.evaluate(() => document.fonts.ready);
    expect(requests.length, 'aucune requete observee').toBeGreaterThan(1);
    expect(requests.some((u) => u.startsWith(`${origin}/static/`)), 'aucune ressource statique chargee').toBe(true);
    expect(requests.filter((u) => !sameOrigin(u, origin)), 'requete hors origine').toEqual([]);
    expect(failures, 'requete echouee').toEqual([]);
    expect(consoleErrors.filter((t) => /block|refused|violat|Content Security|ERR_|Failed to load/i.test(t)), 'erreur de ressource bloquee').toEqual([]);
  });
}
