// Outils des parcours 04 à 06 : un groupe neuf par test (les groupes d'amorçage sont
// partagés avec d'autres specs et ne doivent pas être modifiés), construit par
// l'application réelle en HTTP, jamais par SQL. Les liens personnels viennent de la
// régénération par l'administration ; les paires ne servent qu'à VÉRIFIER les
// résultats côté test : aucune page d'administration ne les affiche.
import { expect, type Page } from '@playwright/test';
import { Client, csrfToken, expectStatus, unescapeHTML } from '../../_shared/http';
import { freshIP, loginAdmin, seed, type Group } from '../../_shared/seed';

export interface Member {
  id: number;
  firstName: string;
  lastName: string;
  name: string;
  email: string;
}

export interface TestGroup {
  slug: string;
  name: string;
  adminSecret: string;
  admin: Client;
  people: Member[];
}

const FIRST = ['Alix', 'Bastien', 'Cléo', 'Dorian', 'Éloïse', 'Farid', 'Gina', 'Hugo', 'Iris'];

export const asGroup = (g: TestGroup): Group => ({
  slug: g.slug,
  name: g.name,
  budget: '',
  adminSecret: g.adminSecret,
  invitePath: '',
  people: [],
  pending: [],
});

export const loginAs = (page: Page, g: TestGroup) => loginAdmin(page, asGroup(g));

/** Suffixe unique par appel (deux projets Playwright tournent en parallèle). */
export const tag = () => Date.now().toString(36).slice(-4) + Math.random().toString(36).slice(2, 5);

export function mailOf(first: string, slug: string): string {
  return `${first.normalize('NFD').replace(/[^a-zA-Z]/g, '').toLowerCase()}.${slug}@exemple.test`;
}

/** Crée le groupe par /creer et ouvre la session d'administration. */
export async function createGroup(kind: string, baseUrl = seed().baseUrl, instancePassword = seed().instancePassword): Promise<TestGroup> {
  const t = tag();
  const slug = `j${kind}-${t}`;
  const name = `Parcours ${kind} ${t}`;
  const admin = new Client(baseUrl, freshIP());
  const created = expectStatus(
    `création de ${slug}`,
    await admin.submit('/creer', '/creer', { password: instancePassword, name, slug, budget: '25' }),
    200,
  );
  const secret = /id="admin-secret">([^<]+)</.exec(created.body)?.[1];
  if (!secret) throw new Error(`secret administrateur de ${slug} introuvable`);
  const adminSecret = unescapeHTML(secret).trim();
  expectStatus(
    'connexion administrateur',
    await admin.submit(`/${slug}/admin/connexion`, `/${slug}/admin/connexion`, { secret: adminSecret }),
    303,
  );
  return { slug, name, adminSecret, admin, people: [] };
}

/** L'administration ajoute une personne (formulaire de la liste) et relit son identifiant. */
export async function addPerson(g: TestGroup, first: string, last: string, email: string): Promise<Member> {
  const base = `/${g.slug}/admin/participants`;
  expectStatus(`ajout de ${first}`, await g.admin.submit(base, base, { first_name: first, name: last, email, gift_hint: '' }), 303);
  const list = expectStatus('liste', await g.admin.get(base), 200);
  const name = `${first} ${last}`;
  for (const m of list.body.matchAll(/<a class="ppl-name" href="\/[^"/]+\/admin\/participants\/(\d+)"[^>]*>([^<]*)</g)) {
    const shown = unescapeHTML(m[2]).replace(/\s+/g, ' ').trim();
    if (shown === name || shown === `${last} ${first}`) {
      const member = { id: Number(m[1]), firstName: first, lastName: last, name, email };
      g.people.push(member);
      return member;
    }
  }
  throw new Error(`${name} absent de la liste d'administration`);
}

/** Un groupe de n personnes avec adresse, tirage non lancé. */
export async function groupWithPeople(kind: string, n: number): Promise<TestGroup> {
  const g = await createGroup(kind);
  for (let i = 0; i < n; i++) await addPerson(g, FIRST[i], 'Parcours', mailOf(FIRST[i], g.slug));
  return g;
}

export async function launchDraw(g: TestGroup): Promise<void> {
  expectStatus(
    'tirage',
    await g.admin.submit(`/${g.slug}/admin/tirage?confirmer=lancer`, `/${g.slug}/admin/tirage/lancer`, { confirm: '1' }),
    303,
  );
}

/** Lien personnel (chemin) d'une personne : l'administration le régénère, comme l'écran du lien remis. */
export async function personalPath(g: TestGroup, m: Member): Promise<string> {
  const page = `/${g.slug}/admin/participants/${m.id}`;
  const r = expectStatus('régénération du lien', await g.admin.submit(page, `${page}/regenerer`, {}), 200);
  const link = /value="([^"]*\/acces\/[^"]+)"/.exec(unescapeHTML(r.body))?.[1];
  if (!link) throw new Error(`lien personnel de ${m.name} introuvable`);
  return new URL(link, 'http://x.invalid').pathname;
}

/** Les cibles d'une personne, lues comme elle les lit (session ouverte par son lien). */
export async function targetsVia(baseUrl: string, g: TestGroup, path: string): Promise<string[]> {
  const c = new Client(baseUrl, freshIP());
  expectStatus('ouverture du lien', await c.submit(path, path, {}), 303);
  const r = expectStatus('cibles', await c.get(`/${g.slug}/decouverte/cibles`), 200);
  return (JSON.parse(r.body) as { targets: { name: string }[] }).targets.map((t) => t.name);
}

/** Toutes les paires du groupe (donnée de contrôle du test, jamais lue dans une page d'administration). */
export async function pairsOf(g: TestGroup, baseUrl = seed().baseUrl): Promise<Map<string, string[]>> {
  const out = new Map<string, string[]>();
  for (const m of g.people) {
    const link = await personalPath(g, m);
    out.set(m.name, await targetsVia(baseUrl, g, link));
  }
  return out;
}

/** Texte visible d'une page lue sans navigateur (balises retirées). */
export function visibleText(html: string): string {
  return unescapeHTML(html.replace(/<script[\s\S]*?<\/script>/g, ' ').replace(/<[^>]+>/g, ' ')).replace(/\s+/g, ' ');
}

export { csrfToken, expect };
