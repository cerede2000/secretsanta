import { expect, test, type Page } from '@playwright/test';
import { expectCleanAdminPage, trackDialogs } from '../../_shared/admin';
import { lane, loginAdmin, type Group } from '../../_shared/seed';

// F10.AC24 : TOUTES les pages d'administration n'ont aucun defilement
// horizontal et leurs zones tactiles mesurent au moins 44 px en 360x640 et
// 390x844 ; aucun attribut style en ligne, aucune boite alert/confirm, aucune
// violation de la CSP ni erreur de script. Les pages sont lues dans les quatre
// etats du groupe (avant tirage, inscriptions en attente, tirage programme,
// revelation ouverte) avec les donnees d'amorcage (_shared/seed.ts).
//
// Les reglages, les relances, l'ajustement et les donnees (vagues 10 a 12) sont
// parcourus ici ; la page d'aide n'existe pas encore : son lien est verifie par
// le test Go TestDefaultInternalLinksReachARoute.

type Entry = { path: string; statuses?: number[]; label: string };

const COMMON: Entry[] = [
  { label: 'tableau de bord', path: 'admin' },
  { label: 'participants', path: 'admin/participants' },
  { label: 'tirage', path: 'admin/tirage' },
  { label: 'tirage : ajuster', path: 'admin/tirage?onglet=ajuster' },
  { label: 'tirage : historique', path: 'admin/tirage?onglet=historique' },
  { label: 'roles', path: 'admin/roles' },
  { label: 'controle du tirage', path: 'admin/controle', statuses: [200, 409] },
  { label: 'journal', path: 'admin/journal' },
  { label: 'menu Plus', path: 'admin/plus' },
  { label: 'inscriptions', path: 'admin/inscriptions' },
  { label: 'reglages', path: 'admin/reglages' },
  { label: 'relances', path: 'admin/relances' },
  { label: 'ajustement : retrait', path: 'admin/ajuster' },
  { label: 'ajustement : ajout', path: 'admin/ajuster?mode=ajouter' },
  { label: 'donnees', path: 'admin/donnees' },
];

function watch(page: Page): string[] {
  const problems: string[] = [];
  page.on('console', (m) => {
    if (/content security policy|refused to|failed to load resource/i.test(m.text())) problems.push(m.text());
  });
  page.on('pageerror', (e) => problems.push(e.message));
  return problems;
}

async function visit(page: Page, g: Group, e: Entry, dialogs: () => number, problems: string[]): Promise<void> {
  const res = await page.goto(`/${g.slug}/${e.path}`);
  expect(e.statuses ?? [200], `${e.path} (${g.slug})`).toContain(res!.status());
  await expect(page.locator('main')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
  // Un statut 409 attendu (controle avant tirage) est journalise par le navigateur comme ressource en echec.
  const real = e.statuses?.includes(409) ? problems.filter((m) => !/failed to load resource.*409/i.test(m)) : problems;
  expect(real, `erreurs de console sur ${e.path}`).toEqual([]);
  problems.length = 0;
}

for (const kind of ['people', 'reg', 'waiting', 'revealed'] as const) {
  test(`pages d'administration, groupe « ${kind} » : sans debordement, cibles de 44 px, CSP propre`, async ({ page }, testInfo) => {
    const g = lane(testInfo)[kind];
    const dialogs = trackDialogs(page);
    const problems = watch(page);
    await loginAdmin(page, g);
    for (const e of COMMON) await visit(page, g, e, dialogs, problems);
    // Pages d'une personne : fiche, modification, confirmation de suppression.
    const p = g.people[0];
    for (const e of [
      { label: 'fiche', path: `admin/participants/${p.id}` },
      { label: 'modification', path: `admin/participants/${p.id}/modifier` },
      { label: 'suppression', path: `admin/participants?supprimer=${p.id}` },
    ]) {
      await visit(page, g, e, dialogs, problems);
    }
    if (kind === 'people') await visit(page, g, { label: 'lancement', path: 'admin/tirage?confirmer=lancer' }, dialogs, problems);
    if (kind === 'waiting') await visit(page, g, { label: 'ouverture', path: 'admin/tirage?confirmer=ouvrir' }, dialogs, problems);
    if (kind === 'reg') await visit(page, g, { label: 'renouvellement', path: 'admin/inscriptions?confirm=renew' }, dialogs, problems);
  });
}

test('connexion administrateur : sans debordement, cibles de 44 px', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const dialogs = trackDialogs(page);
  const res = await page.goto(`/${g.slug}/admin/connexion`);
  expect(res!.status()).toBe(200);
  await expectCleanAdminPage(page, dialogs);
});

test('une page d\'administration sans session renvoie vers la connexion', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  for (const e of COMMON) {
    await page.goto(`/${g.slug}/${e.path}`);
    await expect(page, e.path).toHaveURL(new RegExp(`/${g.slug}/admin/connexion`));
  }
});

test('1024 px : le tableau de bord de bureau n\'a aucun defilement horizontal', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1024, height: 768 });
  const g = lane(testInfo).revealed;
  await loginAdmin(page, g);
  for (const e of COMMON.filter((x) => x.statuses === undefined)) {
    const res = await page.goto(`/${g.slug}/${e.path}`);
    expect(res!.status()).toBe(200);
    const w = await page.evaluate(() => [document.documentElement.scrollWidth, document.documentElement.clientWidth]);
    expect(w[0], e.path).toBeLessThanOrEqual(w[1]);
  }
});
