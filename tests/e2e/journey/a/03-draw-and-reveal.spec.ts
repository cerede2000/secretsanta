import { expect, test, type Page } from '@playwright/test';
import { adminLogin, createGroupHttp, expectCleanMobile, memberLogin, newVisitor, registerMembers, tag, writeIntroHttp } from './kit';

// F12b.AC3, parcours 03 : tirage lancé par l'administrateur, contrôle du tirage
// sans aucun nom, puis révélation dans les trois styles, forcés l'un après
// l'autre par la graine de test (attribut data-seed de la page de révélation,
// ajouté à la volée côté navigateur : l'image ne contient aucune porte dérobée),
// cible révélée, carte de cible et lettre reçue.

const STYLES = ['gift', 'envelope', 'scratch'] as const;

/** Cherche, par le vrai module de styles, une graine qui donne chaque style au premier tirage. */
async function seedsByStyle(page: Page): Promise<Record<string, number>> {
  return page.evaluate(async () => {
    const { pickStyle, seededRandom } = await import('/static/js/reveal/core/styles.js');
    const out: Record<string, number> = {};
    for (let seed = 1; seed < 200 && Object.keys(out).length < 3; seed++) {
      const style = pickStyle(null, seededRandom(seed));
      if (!(style in out)) out[style] = seed;
    }
    return out;
  });
}

test('parcours 03 : tirage, contrôle sans noms, révélation dans les trois styles, cible et lettre reçue', async ({ browser }, testInfo) => {
  const t = tag(testInfo);
  const f = await createGroupHttp(t, '25');
  await registerMembers(f, 5, t);
  for (const m of f.members) await writeIntroHttp(f, m, `Lettre de ${m.firstName} : un livre et du thé.`);
  const firstNames = f.members.map((m) => m.firstName);
  const names = f.members.map((m) => m.name);

  // --- Tirage par l'administrateur ---
  const adminCtx = await newVisitor(browser, testInfo);
  const a = await adminCtx.newPage();
  await adminLogin(a, f);
  await a.goto(`/${f.slug}/admin/tirage`);
  await expect(a.locator('.drw-card-head .chip-warn').first()).toBeVisible();
  await expectCleanMobile(a);
  await a.goto(`/${f.slug}/admin/tirage?confirmer=lancer`);
  await a.locator('form.drw-confirm button[type="submit"]').click();
  await expect(a.locator('.drw-card-head .chip-ok').first()).toBeVisible();
  await expectCleanMobile(a);

  // Contrôle du tirage : vert, uniquement des comptes, aucun nom de personne.
  for (const path of [`/${f.slug}/admin/tirage`, `/${f.slug}/admin/controle`]) {
    await a.goto(path);
    const text = await a.locator('main').innerText();
    for (const n of firstNames) expect(text, `${path} ne doit nommer personne`).not.toContain(n);
  }
  await expect(a.locator('.dash-alert-ok')).toBeVisible();
  await expect(a.locator('.chip-error')).toHaveCount(0);
  await expectCleanMobile(a);

  // --- Révélation : trois personnes, trois styles forcés ---
  const probeCtx = await newVisitor(browser, testInfo);
  const probe = await probeCtx.newPage();
  await probe.goto(`/${f.slug}`);
  const seeds = await seedsByStyle(probe);
  await probeCtx.close();
  expect(Object.keys(seeds).sort()).toEqual([...STYLES].sort());

  const seen: string[] = [];
  for (const [i, style] of STYLES.entries()) {
    const me = f.members[i];
    const ctx = await newVisitor(browser, testInfo);
    const p = await ctx.newPage();
    await p.route(`**/${f.slug}/decouverte`, async (route) => {
      const res = await route.fetch();
      const html = (await res.text()).replace(' data-reveal ', ` data-reveal data-seed="${seeds[style]}" `);
      await route.fulfill({ response: res, body: html });
    });
    await memberLogin(p, me);
    // La connexion a déjà ouvert la page de révélation une première fois, qui a
    // mémorisé son style (une fois prête) : on efface cette mémoire pour que la
    // graine seule décide.
    await expect(p.locator('[data-reveal]')).toHaveAttribute('data-phase', 'ready');
    await p.evaluate(() => localStorage.clear());
    await p.goto(`/${f.slug}/decouverte`);
    const root = p.locator('[data-reveal]');
    await expect(root).toHaveAttribute('data-phase', 'ready');
    await expect(root).toHaveAttribute('data-style', style);
    seen.push((await root.getAttribute('data-style'))!);
    await expectCleanMobile(p, '[data-role="stage"] button.reveal-open', true);
    await p.locator('[data-role="stage"] button.reveal-open').click();
    await expect(root).toHaveAttribute('data-phase', 'revealed');
    const target = (await p.locator('[data-role="name"]').textContent())!.trim();
    expect(names, 'la cible est une personne du groupe').toContain(target);
    expect(target, 'jamais soi-même').not.toBe(me.name);
    await expectCleanMobile(p, undefined, true);

    // Carte de cible : même personne, lettre reçue lisible.
    await p.goto(`/${f.slug}/participant`);
    await expect(p.locator('.draw-card')).toHaveCount(1);
    const targetFirst = f.members.find((m) => m.name === target)!.firstName;
    await expect(p.locator('.draw-card-name')).toHaveText(targetFirst);
    await expectCleanMobile(p, '.draw-mailbox, .draw-card-actions a');
    await p.locator(`.draw-card a[href^="/${f.slug}/lettre-recue/"]`).click();
    await expect(p.locator('.letter-sheet .letter-intro')).toContainText(`Lettre de ${targetFirst}`);
    await expectCleanMobile(p, '.letter-back');
    await ctx.close();
  }
  expect(new Set(seen).size, 'trois styles différents joués').toBe(3);
  await adminCtx.close();
});
