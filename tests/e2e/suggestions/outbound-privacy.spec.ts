import { createServer, type IncomingMessage, type Server } from 'node:http';
import { expect, test } from '@playwright/test';
import { Client, expectStatus } from '../_shared/http';
import { freshIP, lane, seed, type Group, type Person } from '../_shared/seed';

// F8c.AC7 : parcours complet (groupe créé, participants aux noms et courriels
// reconnaissables, tirage, lettre, suggestions) avec un serveur factice de
// recherche. Toutes les requêtes sortantes de l'application y sont capturées
// (URL, en-têtes, corps) : aucune ne contient un courriel, un nom ni un prénom,
// et les seules requêtes sont les recherches de mots-clés (ou la récupération
// protégée, qui n'envoie ni Referer ni cookie).
//
// L'application de la recette doit être lancée avec SEARXNG_URL pointant vers le
// serveur factice : http://127.0.0.1:<E2E_FAKE_SEARCH_PORT> (18090 par défaut).
// Le groupe d'amorçage (créé, inscrit et tiré par l'application réelle dans
// global-setup) fournit les noms et courriels reconnaissables.

test.describe.configure({ mode: 'serial' });

interface Captured {
  method: string;
  url: string;
  headers: Record<string, string | string[] | undefined>;
  body: string;
}

const PORT = Number(process.env.E2E_FAKE_SEARCH_PORT ?? '18090');
const captured: Captured[] = [];
let server: Server;

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => resolve(Buffer.concat(chunks).toString('utf8')));
  });
}

test.beforeAll(async () => {
  server = createServer(async (req, res) => {
    captured.push({ method: req.method ?? '', url: req.url ?? '', headers: { ...req.headers }, body: await readBody(req) });
    res.setHeader('Content-Type', 'application/json');
    res.end(
      JSON.stringify({
        results: [
          { title: 'Écharpe douce en laine', url: 'https://boutique.example/echarpe', content: 'Écharpe, 24,99 €' },
          { title: 'Coffret à thé de Noël', url: 'https://boutique.example/the', content: 'Coffret, 18 €' },
        ],
      }),
    );
  });
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(PORT, '127.0.0.1', resolve);
  });
});

test.afterAll(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

async function sessionOf(p: Person): Promise<Client> {
  const c = new Client(seed().baseUrl, freshIP());
  expectStatus(`session de ${p.firstName}`, await c.submit(p.personalPath, p.personalPath, {}), 303);
  return c;
}

/** Les paires de cibles de `p` (identifiants lus sur sa boîte aux lettres). */
async function pairsOf(g: Group, p: Person): Promise<{ c: Client; pair: string }> {
  const c = await sessionOf(p);
  const box = expectStatus('boîte aux lettres', await c.get(`/${g.slug}/boite`), 200).body;
  const pair = /\/lettre-recue\/(\d+)/.exec(box)?.[1];
  if (!pair) throw new Error(`aucune cible pour ${p.name}`);
  return { c, pair };
}

test('aucune requête sortante ne contient courriel, nom ni prénom', async ({}, testInfo) => {
  const g = lane(testInfo).revealed;
  // Pas people[0] : sa cible reçoit déjà des idées de letter/mobile.spec.ts (une lettre
  // en contient 5 au plus ; au-delà, l'ajout répond 422). Seuls people[0] et people[1]
  // ont découvert leur cible à l'amorçage.
  const giver = g.people[1];

  // Le tireur lit la lettre de sa cible : la cible y écrit, dans ses idées, des noms,
  // prénoms et courriels de participants (qui ne doivent jamais sortir).
  const { c, pair } = await pairsOf(g, giver);
  const box = expectStatus('boîte', await c.get(`/${g.slug}/boite`), 200).body;
  const received = /href="(\/[^"]*\/lettre-recue\/\d+)"/.exec(box)?.[1];
  expect(received, 'lettre reçue').toBeTruthy();
  const letter = expectStatus('lettre reçue', await c.get(received!), 200).body;
  const kicker = /class="letter-kicker">[^<]*?([^<\s]+)<\/div>/.exec(letter)?.[1];
  const target = g.people.find((p) => p.firstName === kicker);
  if (!target) throw new Error(`cible « ${kicker} » introuvable`);
  const writer = await sessionOf(target);
  const others = g.people.filter((p) => p !== target);
  for (const [title, text] of [
    [`Une écharpe de laine pour ${others[0].firstName} ${others[0].lastName}`, `écrire à ${others[0].email}`],
    ['Livre de randonnée', `de la part de ${others[1].name}`],
    ['Coffret à thé', ''],
  ]) {
    expectStatus(`idée « ${title} »`, await writer.submit(`/${g.slug}/lettre`, `/${g.slug}/lettre/idees`, { title, url: '', text }), 303);
  }

  // Les suggestions : affichage, filtre de prix libre, puis actualisation.
  captured.length = 0;
  const path = `/${g.slug}/suggestions/${pair}`;
  expectStatus('page des suggestions', await c.get(path), 200);
  expectStatus('filtre de prix', await c.get(`${path}?min=10&max=40`), 200);
  expectStatus('actualisation', await c.submit(path, `${path}/actualiser`, {}), 303, 200);

  expect(captured.length, 'le serveur factice de recherche n\'a reçu aucune requête (SEARXNG_URL ?)').toBeGreaterThan(0);

  // Chaque chaîne reconnaissable des participants, en toutes casses.
  const forbidden = new Set<string>();
  for (const p of g.people) {
    for (const s of [p.firstName, p.lastName, p.name, p.email, p.email.split('@')[0]]) {
      if (s && s.length >= 3) forbidden.add(s.toLowerCase());
    }
  }
  for (const req of captured) {
    // Seules les recherches de mots-clés et la récupération protégée sortent.
    expect(req.method, `méthode de ${req.url}`).toBe('GET');
    expect(req.url, `requête inattendue ${req.url}`).toMatch(/^\/(search|images?\/|img\/)/);
    expect(req.headers['referer'], `Referer sur ${req.url}`).toBeUndefined();
    expect(req.headers['cookie'], `cookie sur ${req.url}`).toBeUndefined();
    expect(req.headers['authorization'], `Authorization sur ${req.url}`).toBeUndefined();
    const everything = decodeURIComponent(
      (req.url + ' ' + JSON.stringify(req.headers) + ' ' + req.body).replace(/\+/g, ' '),
    ).toLowerCase();
    for (const f of forbidden) {
      expect(everything.includes(f), `« ${f} » dans la requête sortante ${req.url}`).toBe(false);
    }
    expect(everything, `adresse courriel dans ${req.url}`).not.toMatch(/[a-z0-9._-]+@[a-z0-9-]+\.[a-z]{2,}/);
  }
  expect(captured.some((r) => r.url.startsWith('/search')), 'aucune recherche capturée').toBe(true);
});
