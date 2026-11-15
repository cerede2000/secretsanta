// Aides des parcours 07 et 08 : un groupe neuf par exécution, créé par
// l'application RÉELLE en HTTP (jamais de SQL), comme le fait l'amorçage de
// _shared/seed.ts, mais propre au parcours : les parcours changent les rôles,
// lancent un tirage et ne doivent partager leur groupe avec aucune autre spec.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { Client, csrfToken, expectStatus, unescapeHTML } from '../../_shared/http';
import { freshIP, type Group, type Person } from '../../_shared/seed';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';

const FIRST_NAMES = ['Alix', 'Bastien', 'Cléo', 'Dorian', 'Éloïse', 'Farid', 'Gina', 'Hugo'];

const baseUrl = (): string => (process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080').replace(/\/$/, '');

export function instancePassword(): string {
  const p = process.env.E2E_INSTANCE_PASSWORD ?? process.env.INSTANCE_PASSWORD;
  if (!p) throw new Error('E2E_INSTANCE_PASSWORD absent : lancer le parcours par scripts/e2e.sh');
  return p;
}

/** Suffixe unique par exécution et par projet : slug, noms et adresses ne se croisent jamais. */
export function uniqueTag(project: string): string {
  const p = project.replace(/\D/g, '').slice(0, 3);
  return `${p}${Date.now().toString(36).slice(-4)}${Math.random().toString(36).slice(2, 4)}`;
}

export interface Fixture {
  group: Group;
  /** Session d'administration HTTP du groupe (pour les gestes préparatoires). */
  admin: Client;
}

/**
 * Crée le groupe (/creer protégé par le mot de passe d'instance), ouvre la session
 * d'administration, active le lien d'invitation, inscrit `n` personnes par le
 * formulaire public (code lu dans le serveur SMTP factice) et lit leurs
 * identifiants et leurs liens personnels (courriel de bienvenue).
 */
export async function createGroupWithPeople(tag: string, label: string, n: number): Promise<Fixture> {
  const base = baseUrl();
  const slug = `j${label}-${tag}`.toLowerCase();
  const name = `Parcours ${label} ${tag}`;
  const admin = new Client(base, freshIP());
  const created = expectStatus(
    `création de ${slug}`,
    await admin.submit('/creer', '/creer', { password: instancePassword(), name, slug, budget: '25' }),
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
  expectStatus(
    'activation du lien',
    await admin.submit(`/${slug}/admin/inscriptions`, `/${slug}/admin/inscriptions/mode`, { enabled: '1' }),
    303,
  );
  const page = expectStatus('page des inscriptions', await admin.get(`/${slug}/admin/inscriptions?ok=enabled`), 200);
  const invite = new RegExp(`/${slug}/rejoindre/[A-Za-z0-9_-]+`).exec(unescapeHTML(page.body))?.[0];
  if (!invite) throw new Error(`lien d'invitation de ${slug} introuvable`);

  const raw = Array.from({ length: n }, (_, i) => {
    const first = FIRST_NAMES[i];
    return { first, last: 'Parcours', email: `${first.normalize('NFD').replace(/[^a-zA-Z]/g, '').toLowerCase()}.${slug}@exemple.test` };
  });
  await Promise.all(
    raw.map(async (p) => {
      const visitor = new Client(base, freshIP());
      const sent = expectStatus(
        `inscription de ${p.email}`,
        await visitor.submit(invite, invite, { prenom: p.first, nom: p.last, email: p.email }),
        200,
      );
      const mail = await waitForMailWhere(
        (m) => m.To.some((t) => t.Address === p.email) && /v[ée]rification|verification/i.test(m.Subject),
      );
      const code = extractVerificationCode(mail);
      const ref = /name="ref" value="([^"]+)"/.exec(sent.body)?.[1];
      if (!code || !ref) throw new Error(`code ou référence d'inscription introuvable pour ${p.email}`);
      expectStatus(
        `code de ${p.email}`,
        await visitor.postForm(`/${slug}/inscription/code`, { csrf_token: csrfToken(sent.body), ref, retour: invite, code }),
        200,
      );
    }),
  );

  const list = expectStatus('liste des participants', await admin.get(`/${slug}/admin/participants`), 200);
  const ids = new Map<string, number>();
  for (const m of list.body.matchAll(/<a class="ppl-name" href="\/[^"/]+\/admin\/participants\/(\d+)"[^>]*>([^<]*)</g)) {
    ids.set(unescapeHTML(m[2]).replace(/\s+/g, ' ').trim(), Number(m[1]));
  }
  const people: Person[] = await Promise.all(
    raw.map(async (p) => {
      const full = `${p.first} ${p.last}`;
      const id = ids.get(full) ?? ids.get(`${p.last} ${p.first}`);
      if (id === undefined) throw new Error(`${full} absent de la liste d'administration de ${slug}`);
      const welcome = await waitForMailWhere(
        (m) => m.To.some((t) => t.Address === p.email) && /bienvenue|welcome/i.test(m.Subject),
      );
      const link = new RegExp(`/${slug}/acces/[A-Za-z0-9_-]+`).exec(welcome.Text)?.[0];
      if (!link) throw new Error(`lien personnel de ${p.email} introuvable`);
      return { id, firstName: p.first, lastName: p.last, name: full, email: p.email, role: 'normal' as const, personalPath: link };
    }),
  );
  return { group: { slug, name, budget: '25', adminSecret, invitePath: invite, people, pending: [] }, admin };
}

/** Ouvre une session de participant en HTTP (lien personnel), sans navigateur. */
export async function personClient(p: Person): Promise<Client> {
  const c = new Client(baseUrl(), freshIP());
  expectStatus('lien personnel', await c.submit(p.personalPath, p.personalPath, {}), 303);
  return c;
}

/** La personne écrit sa lettre (message d'introduction) par la page réelle. */
export async function writeLetter(g: Group, p: Person, intro: string): Promise<void> {
  const c = await personClient(p);
  expectStatus(`lettre de ${p.firstName}`, await c.submit(`/${g.slug}/lettre`, `/${g.slug}/lettre`, { intro }), 200, 303);
}

/**
 * Exclusion mutuelle entre processus de test : l'API de chaos du serveur SMTP
 * factice est globale (elle refuserait aussi les courriels des autres specs).
 * Les deux projets Playwright tournent en parallèle dans deux processus : un
 * répertoire créé de façon atomique sert de verrou.
 */
export async function withMailLock<T>(fn: () => Promise<T>): Promise<T> {
  const dir = path.join(os.tmpdir(), 'ss-e2e-mail-chaos.lock');
  const deadline = Date.now() + 120_000;
  for (;;) {
    try {
      fs.mkdirSync(dir);
      break;
    } catch (e) {
      if ((e as NodeJS.ErrnoException).code !== 'EEXIST') throw e;
      try {
        if (Date.now() - fs.statSync(dir).mtimeMs > 60_000) fs.rmdirSync(dir); // verrou abandonné
      } catch {
        /* un autre processus vient de le rendre */
      }
      if (Date.now() > deadline) throw new Error('verrou du serveur SMTP factice jamais obtenu');
      await new Promise((r) => setTimeout(r, 150));
    }
  }
  try {
    return await fn();
  } finally {
    fs.rmdirSync(dir);
  }
}
