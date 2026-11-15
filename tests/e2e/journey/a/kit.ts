// Outils communs des parcours 01 à 03 (F12b.1). Chaque parcours crée SES propres
// données (groupe neuf, personnes neuves) : aucun parcours ne dépend de
// l'amorçage ni d'un autre parcours, ils peuvent donc tourner en parallèle dans
// les deux profils mobiles. Tout passe par l'application réelle (HTTP ou
// navigateur) et par la lecture du serveur SMTP factice ; jamais par la base.
import { expect, type Browser, type BrowserContext, type Page, type TestInfo } from '@playwright/test';
import { Client, csrfToken, expectStatus, unescapeHTML } from '../../_shared/http';
import { expectNoHorizontalScroll, expectTouchTargets } from '../../_shared/mobile';
import { asNewVisitor, freshIP } from '../../_shared/seed';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';

export interface Member {
  id: number;
  firstName: string;
  lastName: string;
  name: string;
  email: string;
  personalPath: string;
}

export interface Fixture {
  slug: string;
  name: string;
  adminSecret: string;
  invitePath: string;
  admin: Client;
  members: Member[];
}

const FIRST = ['Alix', 'Bastien', 'Cléo', 'Dorian', 'Éloïse', 'Farid', 'Gina', 'Hugo'];

/** Suffixe unique d'une exécution : lettres minuscules et chiffres. */
export function tag(testInfo: TestInfo): string {
  const p = testInfo.project.name.endsWith('360x640') ? 'a' : 'b';
  return `${p}${Date.now().toString(36).slice(-5)}${Math.random().toString(36).slice(2, 5)}`;
}

/** Contexte de navigateur d'une personne distincte (même format d'écran que le profil). */
export async function newVisitor(browser: Browser, testInfo: TestInfo, lang?: string): Promise<BrowserContext> {
  const use = testInfo.project.use;
  const context = await browser.newContext({
    viewport: use.viewport ?? undefined,
    hasTouch: true,
    isMobile: true,
    locale: lang === 'en' ? 'en-GB' : 'fr-FR',
    baseURL: use.baseURL,
  });
  await asNewVisitor(context);
  if (lang) await context.addCookies([{ name: 'lang', value: lang, url: use.baseURL! }]);
  return context;
}

export async function expectCleanMobile(page: Page, targets?: string, scripted = false): Promise<void> {
  await expectNoHorizontalScroll(page);
  // La page de révélation anime par le CSSOM (autorisé par la CSP) : ses attributs
  // style viennent du script, pas du gabarit ; on ne les compte donc pas.
  if (!scripted) expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
  if (targets) await expectTouchTargets(page.locator(targets));
}

/** Crée un groupe (HTTP), ouvre la session d'administration, active le lien d'invitation. */
export async function createGroupHttp(t: string, budget = ''): Promise<Fixture> {
  const base = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080';
  const pass = process.env.E2E_INSTANCE_PASSWORD ?? '';
  const slug = `j${t}`;
  const name = `Parcours ${t}`;
  const admin = new Client(base, freshIP());
  const created = expectStatus(
    'création du groupe',
    await admin.submit('/creer', '/creer', { password: pass, name, slug, budget }),
    200,
  );
  const secret = /id="admin-secret">([^<]+)</.exec(created.body)?.[1];
  if (!secret) throw new Error('secret administrateur introuvable');
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
  const page = expectStatus('inscriptions', await admin.get(`/${slug}/admin/inscriptions?ok=enabled`), 200);
  const invite = new RegExp(`/${slug}/rejoindre/[A-Za-z0-9_-]+`).exec(unescapeHTML(page.body))?.[0];
  if (!invite) throw new Error("lien d'invitation introuvable");
  return { slug, name, adminSecret, invitePath: invite, admin, members: [] };
}

/** Inscrit n personnes par le formulaire public, codes lus dans le serveur SMTP factice. */
export async function registerMembers(f: Fixture, n: number, t: string): Promise<void> {
  const base = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080';
  const specs = Array.from({ length: n }, (_, i) => ({
    first: FIRST[i],
    last: 'Parcours',
    email: `${FIRST[i].normalize('NFD').replace(/[^a-zA-Z]/g, '').toLowerCase()}.${t}@exemple.test`,
  }));
  await Promise.all(
    specs.map(async (p) => {
      const visitor = new Client(base, freshIP());
      const sent = expectStatus(
        `inscription de ${p.email}`,
        await visitor.submit(f.invitePath, f.invitePath, { prenom: p.first, nom: p.last, email: p.email }),
        200,
      );
      const mail = await waitForMailWhere(
        (m) => m.To.some((x) => x.Address === p.email) && /v[ée]rification|verification/i.test(m.Subject),
      );
      const code = extractVerificationCode(mail);
      const ref = /name="ref" value="([^"]+)"/.exec(sent.body)?.[1];
      if (!code || !ref) throw new Error(`code ou référence introuvable pour ${p.email}`);
      expectStatus(
        `code de ${p.email}`,
        await visitor.postForm(`/${f.slug}/inscription/code`, {
          csrf_token: csrfToken(sent.body),
          ref,
          retour: f.invitePath,
          code,
        }),
        200,
      );
    }),
  );
  const list = expectStatus('participants', await f.admin.get(`/${f.slug}/admin/participants`), 200);
  const ids = new Map<string, number>();
  for (const m of list.body.matchAll(/<a class="ppl-name" href="\/[^"/]+\/admin\/participants\/(\d+)"[^>]*>([^<]*)</g)) {
    ids.set(unescapeHTML(m[2]).replace(/\s+/g, ' ').trim(), Number(m[1]));
  }
  f.members = await Promise.all(
    specs.map(async (p) => {
      const name = `${p.first} ${p.last}`;
      const id = ids.get(name) ?? ids.get(`${p.last} ${p.first}`);
      if (id === undefined) throw new Error(`${name} absent de la liste d'administration`);
      const welcome = await waitForMailWhere(
        (m) => m.To.some((x) => x.Address === p.email) && /bienvenue|welcome/i.test(m.Subject),
      );
      const link = new RegExp(`/${f.slug}/acces/[A-Za-z0-9_-]+`).exec(welcome.Text)?.[0];
      if (!link) throw new Error(`lien personnel de ${p.email} introuvable`);
      return { id, firstName: p.first, lastName: p.last, name, email: p.email, personalPath: link };
    }),
  );
}

/** Connecte la page en administrateur par le formulaire réel. */
export async function adminLogin(page: Page, f: Pick<Fixture, 'slug' | 'adminSecret'>): Promise<void> {
  await page.goto(`/${f.slug}/admin/connexion`);
  await page.locator('input[name="secret"]').fill(f.adminSecret);
  await page.locator('form[data-access-admin] button[type="submit"]').click();
  await page.waitForURL((u) => !u.pathname.endsWith('/admin/connexion'));
}

/** Ouvre la session d'une personne par son lien personnel (« Ouvrir mon espace »). */
export async function memberLogin(page: Page, p: Pick<Member, 'personalPath'>): Promise<void> {
  await page.goto(p.personalPath);
  await page.locator('.access-submit').click();
  await page.waitForURL((u) => !u.pathname.includes('/acces/'));
}

/** Une personne écrit son message au Père Noël par la page réelle (client HTTP, session propre). */
export async function writeIntroHttp(f: Fixture, m: Member, intro: string): Promise<void> {
  const c = new Client(process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080', freshIP());
  expectStatus('session', await c.submit(m.personalPath, m.personalPath, {}), 303);
  expectStatus('lettre', await c.submit(`/${f.slug}/lettre`, `/${f.slug}/lettre`, { intro }), 303);
}
