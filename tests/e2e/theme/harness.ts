import fs from 'node:fs';
import path from 'node:path';
import type { Page } from '@playwright/test';

/** Racine des ressources statiques du depot. */
export const STATIC_DIR = path.resolve(__dirname, '../../../web/static');

export const HARNESS_PATH = '/__theme_harness__';

/** Page d'essai (servie par interception, sans dependre des gabarits) qui consomme le vrai theme servi par l'application. */
const HTML = `<!doctype html><html lang="fr"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/static/css/theme/theme.css">
<script type="module" src="/static/js/theme/snow.js"></script></head>
<body class="theme-page">
<h1 class="theme-title" id="title">Couleurs, texte, formes</h1>
<div class="card" id="card"><p id="body">Texte courant</p><p class="text-secondary" id="secondary">Texte secondaire</p><a class="theme-link" id="link" href="#">Lien</a></div>
<div class="card-admin" id="card-admin"><p id="admin-body">Texte admin</p></div>
<div class="theme-night" id="night"><p id="night-text">Texte attenue</p><h2 class="theme-title" id="night-title">Or</h2></div>
<button class="btn btn-red" id="b-red" type="button">Entrer</button>
<button class="btn btn-green" id="b-green" type="button">Secondaire</button>
<button class="btn btn-ghost" id="b-ghost" type="button">Fantome</button>
<button class="btn btn-danger" id="b-danger" type="button">Supprimer</button>
<button class="btn btn-gold" id="b-gold" type="button">Or sur nuit</button>
<button class="btn btn-ghost-light" id="b-ghost-light" type="button">Fantome clair</button>
<button class="btn" id="b-off" type="button" disabled>Desactive</button>
<label class="field-label" id="label" for="f">Prenom</label><input class="field" id="f" type="text" value="Camille">
<div class="field-error" id="err">Erreur</div>
<span class="chip chip-ok" id="c-ok">Lien ouvert</span><span class="chip chip-warn" id="c-warn">En attente</span><span class="chip chip-error" id="c-error">Echec</span>
</body></html>`;

/** Installe la page d'essai et ouvre-la. Renvoie les requetes observees. */
export async function openHarness(page: Page, requests?: string[]): Promise<void> {
  await page.route(`**${HARNESS_PATH}`, (route) =>
    route.fulfill({ status: 200, contentType: 'text/html; charset=utf-8', body: HTML }),
  );
  if (requests) page.on('request', (r) => requests.push(r.url()));
  await page.goto(HARNESS_PATH);
  await page.waitForLoadState('networkidle');
}

export function readStatic(rel: string): string {
  return fs.readFileSync(path.join(STATIC_DIR, rel), 'utf8');
}

/** Liste recursive des fichiers d'un dossier statique. */
export function walk(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(path.join(dir, e.name)) : [path.join(dir, e.name)],
  );
}

/** Luminance relative WCAG d'une couleur "rgb(r, g, b)" ou "rgba(...)". */
export function luminance(css: string): number {
  const m = css.match(/[\d.]+/g)!.slice(0, 3).map(Number);
  const [r, g, b] = m.map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}
