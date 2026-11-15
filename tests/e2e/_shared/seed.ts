// Amorçage Playwright : les données de départ de toutes les specs.
//
// Tout passe par l'application RÉELLE en HTTP (jamais de SQL) : création des
// groupes par /creer, lien d'inscription activé depuis l'administration, N
// participants inscrits par le formulaire public, code de vérification lu dans
// le courriel capturé (Mailpit du banc officiel, ou puits SMTP maison de
// mail-sink/smtp-sink.mts en local : même API de lecture), rôles réglés par
// l'administration, tirages lancés et cibles découvertes par les pages mêmes.
//
// Les groupes sont répartis en « couloirs » : un par projet Playwright
// (mobile-360x640 et mobile-390x844 tournent en parallèle), pour que deux
// projets ne modifient jamais les mêmes personnes. Une spec obtient ses données
// par `lane(testInfo)` : plus aucune variable E2E_* à fournir à la main.
//
// global-setup.ts appelle `buildSeed()` une fois et publie le résultat dans
// process.env.E2E_SEED ; les specs le relisent par `seed()` / `lane()`.
import { createHmac } from 'node:crypto';
import { expect, type Page, type TestInfo } from '@playwright/test';
import { extractVerificationCode, waitForMailWhere } from '../mail-sink';
import { Client, csrfToken, expectStatus, unescapeHTML } from './http';

export type Role = 'normal' | 'reception_seule' | 'offre_plus' | 'un_seul';

export interface Person {
  /** Identifiant dans le groupe (adresse /<slug>/admin/participants/<id>). */
  id: number;
  firstName: string;
  lastName: string;
  /** Nom affiché dans l'administration : « Prénom Nom ». */
  name: string;
  email: string;
  role: Role;
  /** Lien personnel (/<slug>/acces/<jeton>) du courriel de bienvenue. */
  personalPath: string;
}

/** Une inscription déposée par le formulaire public mais jamais confirmée. */
export interface Pending {
  firstName: string;
  lastName: string;
  email: string;
}

export interface Group {
  slug: string;
  name: string;
  /** Budget du groupe (« 25 »). */
  budget: string;
  /** Secret administrateur, affiché une seule fois à la création. */
  adminSecret: string;
  /** Lien d'invitation actif (/<slug>/rejoindre/<jeton>). */
  invitePath: string;
  people: Person[];
  pending: Pending[];
}

/** Les groupes d'un projet Playwright. */
export interface Lane {
  index: number;
  /** Inscription par lien : 3 personnes confirmées, 3 inscriptions en attente. */
  reg: Group;
  /** Administration des participants : 5 personnes, aucun tirage. */
  people: Group;
  /** Rôles dans le tirage : 5 personnes (la spec les modifie puis les remet). */
  roles: Group;
  /** Avant le tirage : une personne `normal` (préférence) et une `reception_seule`. */
  draft: Group;
  /** Tirage fait, révélation programmée : le compte à rebours. */
  waiting: Group;
  /**
   * Tirage fait, révélation ouverte. `people[0]` (normal) a découvert sa cible ;
   * `people[1]` (offre_plus) a deux cibles dont seule la première est découverte ;
   * `people[2]` (normal) n'a encore rien ouvert ; `people[3]` est reception_seule.
   */
  revealed: Group;
}

export interface Seed {
  baseUrl: string;
  instancePassword: string;
  mailApi: string;
  /** Clé de signature des liens de désinscription (E2E_SIGNING_SECRET), si fournie. */
  signingSecret: string;
  lanes: Lane[];
}

/** Projets Playwright, dans l'ordre des couloirs (playwright.config.ts). */
export const PROJECTS = ['mobile-360x640', 'mobile-390x844'];

const FIRST_NAMES = ['Alix', 'Bastien', 'Cléo', 'Dorian', 'Éloïse', 'Farid', 'Gina', 'Hugo'];

const env = (name: string): string => process.env[name] ?? '';

// ---------------------------------------------------------------------------
// Lecture par les specs

export function seed(): Seed {
  const raw = process.env.E2E_SEED;
  if (!raw) {
    throw new Error(
      "données de départ absentes : lancer la recette par scripts/e2e.sh ou scripts/e2e-local.sh " +
        '(le global-setup les crée avant les specs)',
    );
  }
  return JSON.parse(raw) as Seed;
}

export function lane(testInfo: Pick<TestInfo, 'project'>): Lane {
  const i = Math.max(0, PROJECTS.indexOf(testInfo.project.name));
  return seed().lanes[i];
}

/** Retrouve une personne d'un groupe par son rôle d'amorçage (n-ième de ce rôle). */
export function person(group: Group, role: Role, nth = 0): Person {
  const hit = group.people.filter((p) => p.role === role)[nth];
  if (!hit) throw new Error(`aucune personne de rôle ${role} (#${nth}) dans ${group.slug}`);
  return hit;
}

/** Connecte la page en administrateur du groupe (formulaire de connexion réel). */
export async function loginAdmin(page: Page, group: Group): Promise<void> {
  await page.goto(`/${group.slug}/admin/connexion`);
  await page.locator('input[name="secret"]').fill(group.adminSecret);
  await page.locator('form[data-access-admin] button[type="submit"]').click();
  await page.waitForURL((u) => !u.pathname.endsWith('/admin/connexion'));
}

/** Connecte la page en participant par son lien personnel (« Ouvrir mon espace »). */
export async function loginPerson(page: Page, p: Person): Promise<void> {
  await page.goto(p.personalPath);
  await page.locator('.access-submit').click();
  await page.waitForURL((u) => !u.pathname.includes('/acces/'));
}

/** Génère une adresse IP « cliente » distincte par appel (en-tête X-Forwarded-For). */
let ipCounter = Math.floor(Math.random() * 60000);
export function freshIP(): string {
  ipCounter += 1;
  return `10.${(ipCounter >> 8) % 250}.${ipCounter % 250}.${(ipCounter >> 4) % 250 + 1}`;
}

/**
 * Chaque contexte de navigateur d'une spec qui s'inscrit par le formulaire public
 * se présente comme une personne distincte : le limiteur d'inscriptions par
 * adresse IP (5 par quart d'heure et par groupe) ne gêne plus la recette.
 */
export async function asNewVisitor(context: { setExtraHTTPHeaders(h: Record<string, string>): Promise<void> }): Promise<void> {
  await context.setExtraHTTPHeaders({ 'X-Forwarded-For': freshIP() });
}

// ---------------------------------------------------------------------------
// Liens de désinscription (screen-desinscription)

const groupIds = new Map<string, number>();

/**
 * Lien de désinscription de `p` pour `type`, signé comme mailunsub.Signer. Le
 * numéro du groupe est retrouvé par la page elle-même (elle affiche le nom du
 * groupe) : aucune lecture de la base.
 */
export async function unsubscribeLink(group: Group, p: Person, type: 'relances' | 'nouvelle_idee'): Promise<string> {
  const s = seed();
  if (!s.signingSecret) throw new Error('E2E_SIGNING_SECRET absent : les scripts e2e.sh et e2e-local.sh le fournissent');
  const key = createHmac('sha256', s.signingSecret).update('secretsanta/mailunsub/v1').digest();
  const b64 = (b: Buffer) => b.toString('base64url');
  const sign = (gid: number) => {
    const payload = `v1|${gid}|${p.id}|${type}`;
    return `/desinscription/${b64(Buffer.from(payload))}.${b64(createHmac('sha256', key).update(payload).digest())}`;
  };
  let gid = groupIds.get(group.slug);
  if (gid === undefined) {
    const c = new Client(s.baseUrl);
    for (let id = 1; id <= 400 && gid === undefined; id++) {
      const r = await c.get(sign(id));
      if (r.status === 200 && unescapeHTML(r.body).includes(group.name)) gid = id;
    }
    if (gid === undefined) throw new Error(`numéro du groupe ${group.slug} introuvable par la page de désinscription`);
    groupIds.set(group.slug, gid);
  }
  return sign(gid);
}

// ---------------------------------------------------------------------------
// Construction (global-setup)

interface Config {
  baseUrl: string;
  instancePassword: string;
}

/** Remplit un tableau avec au plus `limit` tâches simultanées. */
async function pool<T>(limit: number, jobs: (() => Promise<T>)[]): Promise<T[]> {
  const out: T[] = new Array(jobs.length);
  let next = 0;
  await Promise.all(
    Array.from({ length: Math.min(limit, jobs.length) }, async () => {
      for (;;) {
        const i = next++;
        if (i >= jobs.length) return;
        out[i] = await jobs[i]();
      }
    }),
  );
  return out;
}

class Builder {
  constructor(private readonly cfg: Config) {}

  private client(): Client {
    return new Client(this.cfg.baseUrl, freshIP());
  }

  /** Crée le groupe (/creer), ouvre la session d'administration, active le lien d'invitation. */
  private async createGroup(slug: string, name: string): Promise<{ admin: Client; group: Group }> {
    const budget = '25';
    const admin = this.client();
    const created = expectStatus(
      `création de ${slug}`,
      await admin.submit('/creer', '/creer', { password: this.cfg.instancePassword, name, slug, budget }),
      200,
    );
    const secret = /id="admin-secret">([^<]+)</.exec(created.body)?.[1];
    if (!secret) throw new Error(`secret administrateur de ${slug} introuvable`);
    const adminSecret = unescapeHTML(secret).trim();
    expectStatus(
      `connexion administrateur de ${slug}`,
      await admin.submit(`/${slug}/admin/connexion`, `/${slug}/admin/connexion`, { secret: adminSecret }),
      303,
    );
    expectStatus(
      `activation du lien de ${slug}`,
      await admin.submit(`/${slug}/admin/inscriptions`, `/${slug}/admin/inscriptions/mode`, { enabled: '1' }),
      303,
    );
    const page = expectStatus('page des inscriptions', await admin.get(`/${slug}/admin/inscriptions?ok=enabled`), 200);
    const invite = new RegExp(`/${slug}/rejoindre/[A-Za-z0-9_-]+`).exec(unescapeHTML(page.body))?.[0];
    if (!invite) throw new Error(`lien d'invitation de ${slug} introuvable`);
    return {
      admin,
      group: { slug, name, budget, adminSecret, invitePath: invite, people: [], pending: [] },
    };
  }

  /** Dépose l'inscription par le formulaire public ; la confirme avec le code lu dans le courriel. */
  private async register(g: Group, first: string, last: string, email: string, confirm: boolean): Promise<void> {
    const visitor = this.client();
    const sent = expectStatus(
      `inscription de ${email}`,
      await visitor.submit(g.invitePath, g.invitePath, { prenom: first, nom: last, email }),
      200,
    );
    if (!confirm) return;
    const mail = await waitForMailWhere(
      (m) => m.To.some((t) => t.Address === email) && /v[ée]rification|verification/i.test(m.Subject),
    );
    const code = extractVerificationCode(mail);
    if (!code) throw new Error(`code de vérification introuvable pour ${email} :\n${mail.Text}`);
    const ref = /name="ref" value="([^"]+)"/.exec(sent.body)?.[1];
    if (!ref) throw new Error(`référence d'inscription introuvable pour ${email}`);
    expectStatus(
      `code de ${email}`,
      await visitor.postForm(`/${g.slug}/inscription/code`, {
        csrf_token: csrfToken(sent.body),
        ref,
        retour: g.invitePath,
        code,
      }),
      200,
    );
  }

  /** Inscrit n personnes (confirmées) puis lit leurs identifiants et liens personnels. */
  private async populate(admin: Client, g: Group, n: number, pending = 0): Promise<void> {
    const base = g.slug;
    const email = (first: string) =>
      `${first.normalize('NFD').replace(/[^a-zA-Z]/g, '').toLowerCase()}.${base}@exemple.test`;
    const make = (i: number) => ({ first: FIRST_NAMES[i], last: 'Amorce', email: email(FIRST_NAMES[i]) });
    const people = Array.from({ length: n }, (_, i) => make(i));
    const waiting = Array.from({ length: pending }, (_, i) => make(n + i));
    await Promise.all([
      ...people.map((p) => this.register(g, p.first, p.last, p.email, true)),
      ...waiting.map((p) => this.register(g, p.first, p.last, p.email, false)),
    ]);
    g.pending = waiting.map((p) => ({ firstName: p.first, lastName: p.last, email: p.email }));
    const list = expectStatus('liste des participants', await admin.get(`/${g.slug}/admin/participants`), 200);
    const ids = new Map<string, number>();
    for (const m of list.body.matchAll(/<a class="ppl-name" href="\/[^"/]+\/admin\/participants\/(\d+)"[^>]*>([^<]*)</g)) {
      ids.set(unescapeHTML(m[2]).replace(/\s+/g, ' ').trim(), Number(m[1]));
    }
    g.people = await Promise.all(
      people.map(async (p) => {
        const name = `${p.first} ${p.last}`;
        const id = ids.get(name) ?? ids.get(`${p.last} ${p.first}`);
        if (id === undefined) throw new Error(`${name} absent de la liste d'administration de ${g.slug}`);
        const welcome = await waitForMailWhere(
          (m) => m.To.some((t) => t.Address === p.email) && /bienvenue|welcome/i.test(m.Subject),
        );
        const link = new RegExp(`/${g.slug}/acces/[A-Za-z0-9_-]+`).exec(welcome.Text)?.[0];
        if (!link) throw new Error(`lien personnel de ${p.email} introuvable :\n${welcome.Text}`);
        return {
          id,
          firstName: p.first,
          lastName: p.last,
          name,
          email: p.email,
          role: 'normal' as Role,
          personalPath: link,
        };
      }),
    );
  }

  private async setRole(admin: Client, g: Group, who: Person[], role: Role): Promise<void> {
    expectStatus(
      `rôle ${role} dans ${g.slug}`,
      await admin.submit(`/${g.slug}/admin/roles`, `/${g.slug}/admin/roles`, { ids: who.map((p) => String(p.id)), role }),
      303,
    );
    for (const p of who) p.role = role;
  }

  private async draw(admin: Client, g: Group): Promise<void> {
    expectStatus(
      `tirage de ${g.slug}`,
      await admin.submit(`/${g.slug}/admin/tirage?confirmer=lancer`, `/${g.slug}/admin/tirage/lancer`, { confirm: '1' }),
      303,
    );
  }

  /** Une personne ouvre sa session par son lien personnel, puis découvre `count` de ses cibles. */
  private async discover(g: Group, p: Person, count: number): Promise<void> {
    const c = this.client();
    expectStatus('page du lien personnel', await c.submit(p.personalPath, p.personalPath, {}), 303);
    const page = expectStatus('page de découverte', await c.get(`/${g.slug}/decouverte`), 200);
    const csrf = /data-csrf="([^"]+)"/.exec(page.body)?.[1];
    if (!csrf) throw new Error('jeton CSRF de la découverte introuvable');
    const targets = JSON.parse(expectStatus('cibles', await c.get(`/${g.slug}/decouverte/cibles`), 200).body) as {
      targets: { id: number }[];
    };
    for (const t of targets.targets.slice(0, count)) {
      expectStatus('confirmation', await c.postJSON(`/${g.slug}/decouverte/confirmation`, { target_id: t.id }, unescapeHTML(csrf)), 200);
    }
  }

  async lane(index: number): Promise<Lane> {
    const tag = Date.now().toString(36).slice(-4) + Math.random().toString(36).slice(2, 4);
    const slug = (kind: string) => `e${index}${kind}-${tag}`;
    const mk = async (kind: string, label: string, n: number, pending = 0) => {
      const { admin, group } = await this.createGroup(slug(kind), `Recette ${label} ${tag}`);
      await this.populate(admin, group, n, pending);
      return { admin, group };
    };

    const [reg, people, roles, draft, waiting, revealed] = await Promise.all([
      mk('reg', 'inscriptions', 3, 3),
      mk('pep', 'participants', 5),
      mk('rol', 'rôles', 5),
      mk('dra', 'avant tirage', 4),
      mk('att', 'attente', 5),
      mk('rev', 'révélation', 7),
    ]);

    // Avant le tirage : une personne qui ne fait que recevoir.
    await this.setRole(draft.admin, draft.group, [draft.group.people[1]], 'reception_seule');

    // Attente : tirage fait, révélation dans trois jours.
    const inThreeDays = new Date(Date.now() + 3 * 24 * 3600 * 1000);
    const pad = (x: number) => String(x).padStart(2, '0');
    const local = `${inThreeDays.getFullYear()}-${pad(inThreeDays.getMonth() + 1)}-${pad(inThreeDays.getDate())}T${pad(inThreeDays.getHours())}:${pad(inThreeDays.getMinutes())}`;
    expectStatus(
      'date de révélation',
      await waiting.admin.submit(`/${waiting.group.slug}/admin/tirage`, `/${waiting.group.slug}/admin/tirage/date`, { date: local }),
      303,
    );
    await this.draw(waiting.admin, waiting.group);

    // Révélation ouverte : sept personnes, une qui reçoit seulement, une qui offre deux cadeaux.
    const rv = revealed.group;
    await this.setRole(revealed.admin, rv, [rv.people[1]], 'offre_plus');
    await this.setRole(revealed.admin, rv, [rv.people[3]], 'reception_seule');
    await this.draw(revealed.admin, rv);
    await this.discover(rv, rv.people[0], 1);
    await this.discover(rv, rv.people[1], 1);

    return {
      index,
      reg: reg.group,
      people: people.group,
      roles: roles.group,
      draft: draft.group,
      waiting: waiting.group,
      revealed: rv,
    };
  }
}

/** Construit les données de départ de tous les couloirs, par l'application réelle. */
export async function buildSeed(): Promise<Seed> {
  const baseUrl = (env('E2E_BASE_URL') || 'http://127.0.0.1:8080').replace(/\/$/, '');
  const instancePassword = env('E2E_INSTANCE_PASSWORD') || env('INSTANCE_PASSWORD');
  if (!instancePassword) {
    throw new Error("E2E_INSTANCE_PASSWORD absent : lancer la recette par scripts/e2e.sh ou scripts/e2e-local.sh");
  }
  const builder = new Builder({ baseUrl, instancePassword });
  const lanes = await pool(
    PROJECTS.length,
    PROJECTS.map((_, i) => () => builder.lane(i)),
  );
  expect(lanes.length).toBe(PROJECTS.length);
  return {
    baseUrl,
    instancePassword,
    mailApi: env('E2E_MAIL_API'),
    signingSecret: env('E2E_SIGNING_SECRET') || env('SIGNING_SECRET'),
    lanes,
  };
}
