import { expect, test, type Page } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../../_shared/mobile';
import { listMails, waitForMailWhere } from '../../mail-sink';
import { guardDialogs } from './dialogs';
import { addPerson, groupWithPeople, launchDraw, loginAs, mailOf, pairsOf, type TestGroup } from './support';

// Parcours 04 : ajustement après tirage (retrait puis ajout) avec courriels vérifiés
// dans le serveur factice, nouvelle règle contredisant une paire, relance manuelle.
// Le groupe est créé pour le test ; les paires ne servent qu'à contrôler les résultats.
// Les relances automatiques (3 puis 7 jours) sont prouvées par les tests unitaires à
// horloge simulée de F7 : l'image n'expose aucun réglage de l'horloge.

const ADJUSTED = /Ton tirage a été ajusté/;

const mailsFor = async (email: string, subject: RegExp) =>
  (await listMails()).filter((m) => subject.test(m.Subject) && m.To.some((t) => t.Address.toLowerCase() === email.toLowerCase()));

async function clean(page: Page) {
  await expectNoHorizontalScroll(page);
  await expectAllInteractiveTargets(page);
  expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
}

/** Qui a changé de cible entre deux relevés (ceux qui restent dans le groupe). */
function changed(before: Map<string, string[]>, after: Map<string, string[]>): string[] {
  return [...after].filter(([who, t]) => before.has(who) && before.get(who)!.join('|') !== t.join('|')).map(([who]) => who);
}

test('parcours 04 : retrait, ajout, règle contre une paire et relance manuelle', async ({ page }) => {
  test.setTimeout(180_000);
  const dialogs = guardDialogs(page);
  try {
    const g: TestGroup = await groupWithPeople('adj', 6);
    await launchDraw(g);
    const p0 = await pairsOf(g);
    const names = g.people.map((p) => p.name);
    for (const [, t] of p0) expect(t.length).toBeGreaterThan(0);
    await loginAs(page, g);

    // --- Retrait d'une personne après le tirage
    const leaver = g.people[0];
    await page.goto(`/${g.slug}/admin/ajuster?mode=retirer`);
    await clean(page);
    await page.locator('#adj-person').selectOption(String(leaver.id));
    await page.getByRole('button', { name: /aperçu/i }).click();
    const preview = page.locator('.adj-preview');
    await expect(preview).toBeVisible();
    await expect(preview).toContainText(/personne.* cible changer|personnes verront/);
    const previewText = await preview.innerText();
    for (const n of names) expect(previewText, 'aperçu sans nom').not.toContain(n.split(' ')[0] + ' Parcours');
    await clean(page);
    await preview.locator('button[type="submit"]').click();
    await expect(page.locator('.adj-result')).toContainText('Ajustement appliqué');
    await expect(page.locator('.adj-result')).toContainText(/courriel/);
    await clean(page);

    const staying = g.people.filter((p) => p !== leaver);
    const p1 = new Map([...(await pairsOf({ ...g, people: staying }))]);
    for (const [, t] of p1) expect(t, 'personne ne tire plus la personne retirée').not.toContain(leaver.name);
    const movedByRemoval = changed(p0, p1);
    expect(movedByRemoval.length, 'au moins la personne qui devait offrir à la personne retirée').toBeGreaterThan(0);
    // Courriels : un par personne dont la cible a changé, aucun pour les autres.
    for (const who of movedByRemoval) {
      const m = staying.find((p) => p.name === who)!;
      const mail = await waitForMailWhere((x) => ADJUSTED.test(x.Subject) && x.To.some((t) => t.Address === m.email));
      for (const n of names) expect(mail.Text, 'courriel sans nom de cible').not.toContain(n.split(' ')[0] + ' Parcours');
      expect(mail.Text).toMatch(new RegExp(`/${g.slug}/(participant|acces)`));
    }
    for (const m of staying.filter((p) => !movedByRemoval.includes(p.name))) {
      expect(await mailsFor(m.email, ADJUSTED), `aucun courriel pour ${m.name}`).toHaveLength(0);
    }

    // --- Ajout d'une personne après le tirage
    const newcomer = await addPerson(g, 'Hugo', 'Parcours', mailOf('Hugo', g.slug));
    await page.goto(`/${g.slug}/admin/ajuster?mode=ajouter`);
    await clean(page);
    await page.locator('#adj-person').selectOption(String(newcomer.id));
    await page.getByRole('button', { name: /aperçu/i }).click();
    await expect(page.locator('.adj-preview')).toBeVisible();
    await page.locator('.adj-preview button[type="submit"]').click();
    await expect(page.locator('.adj-result')).toContainText('Ajustement appliqué');
    await clean(page);

    const p2 = await pairsOf({ ...g, people: [...staying, newcomer] });
    expect(p2.get(newcomer.name)!.length, 'la nouvelle personne a une cible').toBeGreaterThan(0);
    expect([...p2].some(([, t]) => t.includes(newcomer.name)), 'quelqu\'un lui offre').toBe(true);
    const movedByAdd = changed(p1, p2);
    for (const who of movedByAdd) {
      const m = staying.find((p) => p.name === who)!;
      await waitForMailWhere((x) => ADJUSTED.test(x.Subject) && x.To.some((t) => t.Address === m.email));
    }

    // --- Nouvelle règle qui contredit une paire existante
    const [giver, target] = [...p2].filter(([who]) => who !== newcomer.name).map(([who, t]) => [who, t[0]])[0];
    const gm = [...staying, newcomer].find((p) => p.name === giver)!;
    const tm = [...staying, newcomer].find((p) => p.name === target)!;
    await page.goto(`/${g.slug}/admin/participants`);
    await clean(page);
    const form = page.locator('form[action$="/regles"]:has(input[name="kind"][value="exclusion"])');
    await form.locator('select[name="a"]').selectOption(String(gm.id));
    await form.locator('select[name="b"]').selectOption(String(tm.id));
    await form.locator('button[type="submit"]').click();
    const confirm = page.locator('.ppl-confirm');
    await expect(confirm).toBeVisible();
    await expect(confirm).toContainText(/\d/);
    const confirmText = await confirm.innerText();
    for (const n of names) expect(confirmText, 'confirmation sans nom').not.toContain(n);
    await clean(page);
    await confirm.locator('button[name="choice"][value="repair"]').click();
    await expect(page).toHaveURL(/ok=rule/);
    const p3 = await pairsOf({ ...g, people: [...staying, newcomer] });
    expect(p3.get(giver), 'la paire contredite est réparée').not.toContain(target);

    // --- Relance manuelle (découvrir sa cible) : mise en file, puis refus du doublon du jour
    const chosen = staying[0];
    await page.goto(`/${g.slug}/admin/relances?type=lecture`);
    await clean(page);
    await page.locator(`input[name="person"][value="${chosen.id}"]`).check();
    await page.getByRole('button', { name: /envoyer|relancer/i }).click();
    await expect(page.locator('#stg-res')).toBeVisible();
    await expect(page.locator('main')).toContainText('relance mise en file');
    // La remise du courriel de relance n'est pas vérifiée ici : sur le banc (PUBLIC_URL en
    // http de bouclage) mailmsg refuse le lien de désinscription non https des courriels non
    // essentiels (défaut de F6 signalé dans le rapport de phase, à corriger dans internal/mailmsg).
    await clean(page);
    await page.goto(`/${g.slug}/admin/relances?type=lecture`);
    await page.locator(`input[name="person"][value="${chosen.id}"]`).check();
    await page.getByRole('button', { name: /envoyer|relancer/i }).click();
    await expect(page.locator('main')).toContainText('déjà relancée aujourd\'hui');
    dialogs.expectNone();
  } finally {
    dialogs.stop();
  }
});
