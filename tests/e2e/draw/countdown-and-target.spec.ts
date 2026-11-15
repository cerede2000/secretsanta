import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll, expectTouchTargets } from '../_shared/mobile';
import { lane, loginPerson } from '../_shared/seed';

// F4d.AC2 : les pages du compte a rebours et de la carte de cible, sans
// defilement horizontal et avec des zones tactiles de 44 px, en 360x640 et
// 390x844. Donnees : groupes d'amorcage « waiting » (tirage fait, revelation dans
// trois jours) et « revealed » (revelation ouverte, cibles decouvertes ou non),
// voir _shared/seed.ts.

test('attente : compte a rebours rendu par le serveur, budget, preference verrouillee, aucune cible', async ({ page }, testInfo) => {
  const g = lane(testInfo).waiting;
  await loginPerson(page, g.people[0]);
  await page.goto(`/${g.slug}/participant`);
  const timer = page.locator('[role="timer"][data-countdown]');
  await expect(timer).toBeVisible();
  // Jours, heures, minutes : trois blocs chiffres, l'etiquette lue par les lecteurs d'ecran les reprend.
  await expect(timer.locator('.draw-block')).toHaveCount(3);
  const days = Number(await timer.locator('[data-unit="days"]').textContent());
  expect(days).toBeGreaterThanOrEqual(2);
  await expect(timer).toHaveAttribute('aria-label', new RegExp(`${days} jours`));
  // Budget du groupe affiche puisqu'il est defini.
  await expect(page.locator('.draw-facts')).toContainText('25');
  // La preference est verrouillee apres le tirage, avec son explication.
  const sw = page.locator('.draw-pref [role="switch"]');
  await expect(sw).toHaveAttribute('aria-disabled', 'true');
  await expect(page.locator('.draw-pref-lock')).toBeVisible();
  // Aucune cible avant l'ouverture : pas de lien de decouverte ni de carte.
  await expect(page.locator(`a[href="/${g.slug}/decouverte"]`)).toHaveCount(0);
  await expect(page.locator('.draw-card')).toHaveCount(0);
  expect(await page.locator('[style]').count(), 'style en ligne').toBe(0);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('attente : la cible n\'est jamais servie avant l\'ouverture de la revelation', async ({ page }, testInfo) => {
  const g = lane(testInfo).waiting;
  await loginPerson(page, g.people[1]);
  for (const path of [`/${g.slug}/ma-cible`, `/${g.slug}/decouverte`, `/${g.slug}/decouverte/cibles`]) {
    const res = await page.request.get(path, { maxRedirects: 0 });
    const body = await res.text();
    const names = g.people.map((p) => p.firstName);
    // Aucun prenom du groupe (hors le sien) dans la reponse, et aucune liste de cibles.
    for (const n of names.filter((n) => n !== g.people[1].firstName)) {
      expect(body, `${path} ne doit pas nommer ${n}`).not.toContain(`>${n}<`);
    }
    expect(body, `${path} ne doit pas lister de cibles`).not.toMatch(/"targets"\s*:\s*\[\s*\{/);
  }
});

test('carte de cible : une cible, lettre et idees, budget, cibles tactiles', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  const me = g.people[0];
  await loginPerson(page, me);
  await page.goto(`/${g.slug}/participant`);
  await expect(page.locator('h1.draw-sr')).toHaveText(/Ma cible/);
  const cards = page.locator('.draw-card');
  await expect(cards).toHaveCount(1);
  const name = (await cards.locator('.draw-card-name').textContent())!.trim();
  expect(g.people.map((p) => p.firstName), 'la cible est un membre du groupe').toContain(name);
  expect(name, 'jamais soi-meme').not.toBe(me.firstName);
  await expect(cards.locator('.draw-avatar')).toHaveText(name[0]);
  await expect(cards.locator('.draw-row')).toContainText('25');
  await expect(cards.locator(`a[href^="/${g.slug}/lettre-recue/"]`)).toBeVisible();
  await expect(cards.locator(`a[href^="/${g.slug}/suggestions/"]`)).toBeVisible();
  await expect(page.locator('.draw-mailbox')).toHaveAttribute('href', `/${g.slug}/boite`);
  expect(await page.locator('[style]').count(), 'style en ligne').toBe(0);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  await expectTouchTargets(page.locator('.draw-mailbox, .draw-card-actions a'));
});

test('carte de cible a deux cibles : la seconde reste « a decouvrir » tant que son drapeau n\'est pas pose', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[1]);
  await page.goto(`/${g.slug}/participant`);
  const cards = page.locator('.draw-card');
  await expect(cards).toHaveCount(2);
  await expect(cards.nth(0).locator('.draw-chip')).toHaveText(/1.*2/);
  await expect(cards.nth(0).locator(`a[href^="/${g.slug}/lettre-recue/"]`)).toBeVisible();
  await expect(cards.nth(1).locator('.draw-chip')).toHaveText(/2.*2/);
  await expect(cards.nth(1).locator('.draw-card-todo')).toBeVisible();
  await expect(cards.nth(1).locator('.draw-avatar')).toHaveCount(0);
  await expect(cards.nth(1).locator(`a[href="/${g.slug}/decouverte"]`)).toBeVisible();
  // Rien ne trahit la seconde cible : une seule carte porte un prenom.
  await expect(page.locator('.draw-card-name:not(.draw-card-todo)')).toHaveCount(1);
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
});

test('revelation ouverte mais cible pas encore decouverte : la page mene a la decouverte', async ({ page }, testInfo) => {
  const g = lane(testInfo).revealed;
  await loginPerson(page, g.people[2]);
  await page.goto(`/${g.slug}/participant`);
  await expect(page).toHaveURL(new RegExp(`/${g.slug}/decouverte$`));
  await expect(page.locator('.draw-card')).toHaveCount(0);
  await expectNoHorizontalScroll(page);
});
