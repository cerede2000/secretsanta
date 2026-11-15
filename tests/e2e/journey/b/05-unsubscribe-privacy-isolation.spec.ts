import { expect, test } from '@playwright/test';
import { Client, expectStatus } from '../../_shared/http';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared/mobile';
import { freshIP, seed, unsubscribeLink, type Person } from '../../_shared/seed';
import { guardDialogs } from './dialogs';
import { groupWithPeople, launchDraw, loginAs, pairsOf, personalPath, visibleText, type TestGroup } from './support';

// Parcours 05 : désinscription par lien signé, administrateur sans vue des paires,
// isolation entre deux groupes. Groupes créés pour le test (les paires ne servent qu'à
// contrôler que rien ne les montre).

const asPerson = (m: TestGroup['people'][number]): Person => ({
  id: m.id,
  firstName: m.firstName,
  lastName: m.lastName,
  name: m.name,
  email: m.email,
  role: 'normal',
  personalPath: '',
});

test('parcours 05 : désinscription signée, administration sans paires, isolation des groupes', async ({ page, browser }) => {
  test.setTimeout(180_000);
  const dialogs = guardDialogs(page);
  try {
    const A = await groupWithPeople('priv', 5);
    const B = await groupWithPeople('iso', 4);
    await launchDraw(A);
    await launchDraw(B);
    const pairs = await pairsOf(A);

    // --- Désinscription par lien signé : le chargement ne coupe rien, le bouton oui.
    const quitter = A.people[1];
    const link = await unsubscribeLink(
      { slug: A.slug, name: A.name, budget: '', adminSecret: '', invitePath: '', people: [], pending: [] },
      asPerson(quitter),
      'relances',
    );
    const res = await page.goto(link);
    expect(res?.status()).toBe(200);
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    await expect(page.locator('[role="status"]')).toHaveCount(0);
    expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
    await page.locator('.unsub-form button[type="submit"]').click();
    await expect(page.locator('[role="status"]')).toBeVisible();
    await expectNoHorizontalScroll(page);
    // Une signature falsifiée est refusée de la même façon qu'un lien inconnu.
    expect((await page.goto(link.slice(0, -3) + 'AAA'))?.status()).toBe(404);

    // L'administration le voit : la relance manuelle l'écarte, sans courriel.
    await loginAs(page, A);
    await page.goto(`/${A.slug}/admin/relances?type=lecture`);
    await page.locator(`input[name="person"][value="${quitter.id}"]`).check();
    await page.getByRole('button', { name: /envoyer|relancer/i }).click();
    await expect(page.locator('#stg-res')).toBeVisible();
    await expect(page.locator('main')).toContainText('désinscrite des relances');
    await expectNoHorizontalScroll(page);

    // --- L'administration ne voit jamais qui offre à qui.
    const base = seed().baseUrl;
    const adminPages = [
      '', '/controle', '/journal', '/participants', '/tirage', '/tirage?onglet=ajuster', '/tirage?onglet=historique',
      '/ajuster?mode=retirer', '/ajuster?mode=ajouter', '/reglages', '/relances?type=lecture', '/plus', '/donnees',
      '/roles', '/inscriptions',
      ...A.people.map((p) => `/participants/${p.id}`),
    ];
    const texts: string[] = [];
    for (const path of adminPages) {
      const r = expectStatus(`page ${path}`, await A.admin.get(`/${A.slug}/admin${path}`), 200);
      texts.push(visibleText(r.body));
    }
    const exported = expectStatus('export', await A.admin.get(`/${A.slug}/admin/participants/export`), 200);
    texts.push(exported.body);
    // Les listes de personnes (tableau de bord, participants, rôles, relances, ajustement, journal « pour qui ») montrent
    // des noms sans aucun lien entre eux. Les pages du tirage, du contrôle, des
    // réglages et des données ne montrent aucun nom de participant, donc aucune paire ; et
    // aucune page ne formule « X offre à Y ».
    const noNames = ['/controle', '/tirage', '/reglages', '/plus', '/donnees'];
    for (const [i, t] of texts.entries()) {
      const where = adminPages[i] ?? 'export';
      for (const [giver] of pairs) {
        expect(t, `${where} : ${giver} offre à`).not.toMatch(new RegExp(`${giver}\\s*(offre à|→|->|tire)`));
        if (noNames.includes(where.split('?')[0])) expect(t, `${where} sans nom de participant`).not.toContain(giver.split(' ')[0] + ' Parcours');
      }
    }
    expect(exported.body, 'export des participants sans champ de paire').not.toMatch(/"(giver|receiver|target|pairs?|cible|tireur)"/i);
    // Les points d'accès réservés aux participants refusent l'administrateur.
    for (const path of [`/${A.slug}/decouverte/cibles`, `/${A.slug}/ma-cible`]) {
      const r = await A.admin.get(path);
      expect(r.body, `${path} ne montre aucune cible à l'administrateur`).not.toContain('"targets":[{');
      for (const [, ts] of pairs) for (const t of ts) expect(r.body).not.toContain(t);
    }

    // --- Isolation entre deux groupes.
    // L'administrateur de B n'entre pas dans A, ni avec sa session ni avec son secret.
    const ctxB = await browser.newContext({ viewport: page.viewportSize()!, hasTouch: true, isMobile: true, locale: 'fr-FR', extraHTTPHeaders: { 'X-Forwarded-For': freshIP() } });
    const pageB = await ctxB.newPage();
    const dialogsB = guardDialogs(pageB);
    try {
      await loginAs(pageB, B);
      await expect(pageB.locator('body')).toContainText(B.name);
      const denied = await pageB.goto(`/${A.slug}/admin/participants`);
      const seen = await pageB.locator('body').innerText();
      expect([200, 401, 403, 404].includes(denied!.status())).toBe(true);
      for (const p of A.people) expect(seen, 'les personnes de A restent invisibles pour l\'admin de B').not.toContain(p.firstName);
      expect(pageB.url()).not.toMatch(/\/admin\/participants$/);
      // Le secret de B ne sert pas pour A.
      const c = new Client(base, freshIP());
      const att = await c.submit(`/${A.slug}/admin/connexion`, `/${A.slug}/admin/connexion`, { secret: B.adminSecret });
      expect(att.status, 'secret d\'un autre groupe refusé').not.toBe(303);
      // Un participant de A ne lit ni les cibles ni l'espace de B.
      const pa = await personalPath(A, A.people[0]);
      const cp = new Client(base, freshIP());
      expectStatus('ouverture du lien de A', await cp.submit(pa, pa, {}), 303);
      for (const path of [`/${B.slug}/decouverte/cibles`, `/${B.slug}/participant`, `/${B.slug}/ma-cible`, `/${B.slug}/admin`]) {
        const r = await cp.get(path);
        for (const p of B.people) expect(r.body, `${path} : ${p.firstName}`).not.toContain(p.email);
        expect(r.status === 200 && r.body.includes('"targets":[{'), `${path} ne montre aucune cible de B`).toBe(false);
      }
      // Le lien de A posé sur le groupe B ne mène nulle part.
      const wrongPath = pa.replace(`/${A.slug}/`, `/${B.slug}/`);
      const wrong = new Client(base, freshIP());
      const outcome = await wrong.submit(wrongPath, wrongPath, {}).then((r) => r.status, () => 404);
      expect(outcome, 'le lien de A ne donne aucune session dans B').not.toBe(303);
      expect((await wrong.get(`/${B.slug}/participant`)).status, 'aucune session dans B').not.toBe(200);
      // La page publique d'un groupe ne dit rien de l'autre.
      const pub = await new Client(base, freshIP()).get(`/${B.slug}`);
      expect(pub.body).not.toContain(A.name);
      dialogsB.expectNone();
    } finally {
      dialogsB.stop();
      await ctxB.close();
    }
    dialogs.expectNone();
  } finally {
    dialogs.stop();
  }
});
