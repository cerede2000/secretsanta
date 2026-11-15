import { expect, test } from '@playwright/test';
import { Client, csrfToken, expectStatus } from '../_shared/http';
import { expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { freshIP, lane, loginPerson, seed, type Group, type Person } from '../_shared/seed';

// F5c.AC1 : screen-lettre et screen-lettre-recue, sur le groupe d'amorçage dont
// la révélation est ouverte (par l'application réelle), n'ont aucun défilement
// horizontal et toutes leurs cibles tactiles mesurent au moins 44 px en
// 360x640 et 390x844. La personne 0 a découvert une cible ; la personne 1 en a
// deux dont une seule découverte. La lettre de la cible est remplie par les
// pages réelles (HTTP) avant les lectures.

test.describe.configure({ mode: 'serial' });

const LONG_INTRO =
  "Cher Père Noël, cette année j'ai été sage, ou presque. J'aimerais une écharpe très longue, un livre d'aventure et quelque chose pour le thé. " +
  'Supercalifragilisticexpialidocious_supercalifragilisticexpialidocious_supercalifragilisticexpialidocious';

/** Ouvre une session de `p` avec un client HTTP. */
async function sessionOf(g: Group, p: Person): Promise<Client> {
  const c = new Client(seed().baseUrl, freshIP());
  expectStatus(`session de ${p.firstName}`, await c.submit(p.personalPath, p.personalPath, {}), 303);
  return c;
}

/** Les adresses des lettres reçues, lues sur la boîte aux lettres de `p`. */
async function receivedLinks(g: Group, p: Person): Promise<{ href: string; name: string }[]> {
  const c = await sessionOf(g, p);
  const box = expectStatus('boîte aux lettres', await c.get(`/${g.slug}/boite`), 200).body;
  const hrefs = [...box.matchAll(/href="(\/[^"]*\/lettre-recue\/\d+)"/g)].map((m) => m[1]);
  const out: { href: string; name: string }[] = [];
  for (const href of hrefs) {
    const body = expectStatus('lettre reçue', await c.get(href), 200).body;
    const kicker = /class="letter-kicker">[^<]*?([^<\s]+)<\/div>/.exec(body)?.[1];
    if (!kicker) throw new Error(`nom de la lettre ${href} introuvable`);
    out.push({ href, name: kicker });
  }
  return out;
}

/** Prénom de la cible de la personne 0, dont la lettre est remplie par beforeAll. */
let filledFirstName = '';

test.beforeAll(async ({}, testInfo) => {
  const g = lane(testInfo).revealed;
  // La cible de la personne 0 écrit sa lettre : un long message, trois idées dont
  // une avec un lien et une note.
  const [first] = await receivedLinks(g, g.people[0]);
  const target = g.people.find((p) => p.firstName === first.name);
  if (!target) throw new Error(`cible « ${first.name} » introuvable parmi les personnes du groupe`);
  filledFirstName = target.firstName;
  const c = await sessionOf(g, target);
  expectStatus('message de la cible', await c.submit(`/${g.slug}/lettre`, `/${g.slug}/lettre`, { intro: LONG_INTRO }), 303);
  for (const [title, url, text] of [
    ['Écharpe très longue en laine mérinos', 'https://exemple.test/produits/echarpe-tres-longue-laine-merinos-gris-chine?couleur=gris', 'Plutôt grise ou bleue, taille unique.'],
    ['Livre d\'aventure', '', ''],
    ['Coffret à thé', '', 'Sans théine si possible.'],
  ]) {
    expectStatus(`idée « ${title} »`, await c.submit(`/${g.slug}/lettre`, `/${g.slug}/lettre/idees`, { title, url, text }), 303);
  }
});

test('screen-lettre : lettre blanche puis remplie, sans défilement horizontal ni cible de moins de 44 px', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  // Une personne dont la lettre n'a pas été remplie par beforeAll.
  const writer = g.people.find((p) => p.role !== 'reception_seule' && p.firstName !== filledFirstName);
  if (!writer) throw new Error('aucune personne disponible pour écrire');
  await loginPerson(page, writer);

  // Lettre blanche, avec le bandeau de bienvenue.
  await page.goto(`/${g.slug}/lettre?bienvenue=1`);
  await expect(page.locator('.letter-welcome')).toBeVisible();
  await expect(page.locator('.letter-empty')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.letter-back, .letter-send-btn, .letter-add, .letter-photo, .letter-input, .letter-ta'));

  // Message long et idée ajoutée par les vrais formulaires.
  await page.locator('#intro').fill(LONG_INTRO);
  await page.locator('.letter-send-btn').click();
  await page.waitForURL(`**/${g.slug}/lettre`);
  await page.locator('#idea-title').fill('Écharpe très longue en laine mérinos qui descend jusqu\'aux chevilles');
  await page.locator('#idea-url').fill('https://exemple.test/produits/echarpe-tres-longue-laine-merinos-gris-chine?couleur=gris');
  await page.locator('#idea-text').fill('Plutôt grise ou bleue, taille unique.');
  await page.locator('.letter-add').click();
  await page.waitForURL(`**/${g.slug}/lettre`);
  await expect(page.locator('.letter-idea')).toHaveCount(1);
  await expect(page.locator('.letter-saved')).toContainText('Enregistré');

  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.letter-back, .letter-del, .letter-send-btn, .letter-add, .letter-photo, .letter-input, .letter-ta'));
  expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
});

test('screen-lettre-recue : lettre remplie de la cible, sans défilement horizontal ni cible de moins de 44 px', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[0]);
  const [{ href }] = await receivedLinks(g, g.people[0]);
  await page.goto(href);
  await expect(page.locator('.letter-sheet .letter-intro')).toContainText('Cher Père Noël, cette année');
  expect(await page.locator('.letter-idea-read').count()).toBeGreaterThanOrEqual(3);
  await expect(page.locator('.letter-silent')).toBeVisible();
  await expect(page.locator('.letter form, .letter-send')).toHaveCount(0);
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.letter-back'));
  expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
});

test('screen-lettre-recue : deux cibles, onglet du cadeau découvert', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[1]);
  const [{ href }] = await receivedLinks(g, g.people[1]);
  await page.goto(href);
  await expect(page.locator('nav.letter-tabs .letter-tab')).toHaveCount(1);
  await expect(page.locator('.letter-tab[aria-current="page"]')).toBeVisible();
  await expectNoHorizontalScroll(page);
  await expectTouchTargets(page.locator('.letter-back, .letter-tab'));
});
