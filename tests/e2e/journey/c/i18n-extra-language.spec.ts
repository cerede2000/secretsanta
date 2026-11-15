import { expect, test, type Page } from '@playwright/test';
import { expectNoHorizontalScroll } from '../../_shared/mobile';
import { asNewVisitor, loginAdmin, loginPerson } from '../../_shared/seed';
import { extractVerificationCode, waitForMailWhere } from '../../mail-sink';
import { createGroupWithPeople, instancePassword, setLang, uniqueTag, writeLetter } from './support';

// Parcours i18n (F12b.AC17), dans le conteneur :
//  1. le parcours est joué en anglais (cookie lang=en) sur les écrans principaux (accueil,
//     création de groupe, inscription, espace participant, page lettre, découverte) :
//     ni clé brute (« xxx.yyy.zzz » non traduit) ni texte français visible ;
//  2. une langue ajoutée par un fichier plat dans I18N_DIR (volume monté par
//     docker-compose.test.yml : tests/e2e/fixtures/i18n/xx.json, code « xx », nom
//     « Testlandia », chaque texte anglais préfixé de « [xx] ») apparaît dans le
//     sélecteur, s'applique aux pages et au courriel de vérification lu dans le
//     serveur SMTP factice.
// Aucune boîte de dialogue du navigateur n'est ouverte : les écouteurs de dialogue
// ne sont jamais posés (une boîte serait refusée en silence par Playwright).

const RAW_KEY = /\b[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*){2,}\b/g;
const FRENCH =
  /\b(bienvenue|commencer|enregistr\w*|p[èe]re no[ëe]l|cr[ée]er|pr[ée]nom|mot de passe|aller au contenu|votre|vous|retour|valider|envoyer|suivant|ajouter|supprimer|cadeaux?|tirage|inscription|lettre|ouvrir|d[ée]couvrir)\b/i;

/** Texte visible de la page, sans clé brute ni mot français ; la langue du document est celle attendue. */
async function expectLanguage(page: Page, what: string, lang: string): Promise<void> {
  await expect(page.locator('html'), `${what} : langue du document`).toHaveAttribute('lang', lang);
  const text = `${await page.title()}\n${await page.locator('body').innerText()}`;
  expect(text.match(RAW_KEY) ?? [], `${what} : clé brute visible`).toEqual([]);
  expect(FRENCH.exec(text)?.[0] ?? null, `${what} : texte français visible`).toBeNull();
  await expectNoHorizontalScroll(page);
}

test('parcours i18n 1 : anglais sur les écrans principaux, sans clé brute ni français', async ({ page }, testInfo) => {
  test.setTimeout(240_000);
  const tag = uniqueTag(testInfo.project.name);
  const { group: g } = await createGroupWithPeople(tag, 'en', 4);
  for (const p of g.people) await writeLetter(g, p, `Hello, this is ${p.firstName}: a book and some tea.`);
  const ctx = page.context();
  await asNewVisitor(ctx);
  await setLang(ctx, 'en');

  // Accueil, création d'un groupe par le formulaire.
  await page.goto('/');
  await expectLanguage(page, 'accueil', 'en');
  await page.locator('main a.button').first().click();
  await expect(page).toHaveURL(/\/creer$/);
  await expectLanguage(page, 'création de groupe (formulaire)', 'en');
  await page.fill('#g-password', instancePassword());
  await page.fill('#g-name', `Winter exchange ${tag}`);
  await page.fill('#g-slug', `w${tag}`.toLowerCase());
  await page.locator('form.groups-form button[type="submit"]').click();
  await expect(page.locator('#admin-secret')).toBeVisible();
  await expectLanguage(page, 'création de groupe (réussite)', 'en');

  // Inscription : formulaire, saisie du code lu dans le serveur SMTP factice, réussite.
  const email = `gaspard.${g.slug}@exemple.test`;
  await page.goto(g.invitePath);
  await expectLanguage(page, 'inscription (formulaire)', 'en');
  await page.locator('input[name="prenom"]').fill('Gaspard');
  await page.locator('input[name="nom"]').fill('Parcours');
  await page.locator('input[name="email"]').fill(email);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('input[name="code"]')).toBeVisible();
  await expectLanguage(page, 'inscription (code)', 'en');
  const mail = await waitForMailWhere((m) => m.To.some((t) => t.Address === email) && /verification code/i.test(m.Subject));
  const code = extractVerificationCode(mail);
  expect(code, 'code de vérification').not.toBeNull();
  // Le pied du courriel anglais nomme l'application « Père Noël secret » (nom propre, du catalogue anglais) : toléré ; les adresses ne comptent pas (chemins français).
  expect(mail.Text.replace(/https?:\/\/\S+/g, '').replace(/Père Noël secret/g, ''), 'courriel de vérification en anglais').not.toMatch(FRENCH);
  await page.locator('input[name="code"]').fill(code!);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('input[name="code"]')).toHaveCount(0);
  await expectLanguage(page, 'inscription (réussite)', 'en');

  // Tirage par l'administration (écrans d'administration hors périmètre).
  await ctx.clearCookies();
  await loginAdmin(page, g);
  await page.goto(`/${g.slug}/admin/tirage?confirmer=lancer`);
  await page.locator('form[action$="/admin/tirage/lancer"] button[type="submit"]').click();
  await expect(page.locator('form[action$="/admin/tirage/lancer"]')).toHaveCount(0);
  const open = page.locator('a[href$="confirmer=ouvrir"]');
  if (await open.count()) {
    await open.click();
    await page.locator('form[action$="/admin/tirage/ouvrir"] button[type="submit"]').click();
  }

  // Espace participant, lettre, découverte, lettre reçue, en anglais.
  await ctx.clearCookies();
  await setLang(ctx, 'en');
  await loginPerson(page, g.people[0]);
  await page.goto(`/${g.slug}/participant`);
  await expectLanguage(page, 'espace participant', 'en');
  await page.goto(`/${g.slug}/lettre`);
  await expectLanguage(page, 'page lettre', 'en');
  await page.goto(`/${g.slug}/decouverte`);
  const root = page.locator('[data-reveal]');
  await expect(root).toHaveAttribute('data-phase', 'ready');
  await expectLanguage(page, 'découverte (avant)', 'en');
  await page.locator('[data-role="stage"] button.reveal-open').click();
  await expect(root).toHaveAttribute('data-phase', 'revealed');
  await expectLanguage(page, 'découverte (cible révélée)', 'en');
  await page.locator('[data-role="letter"]').click();
  await expect(page.locator('.letter-sheet .letter-intro')).toContainText(/Hello, this is|This letter is still blank/);
  await expectLanguage(page, 'lettre reçue', 'en');
});

test('parcours i18n 2 : une langue ajoutée par I18N_DIR, dans le sélecteur, les pages et un courriel', async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const tag = uniqueTag(testInfo.project.name);
  const { group: g } = await createGroupWithPeople(tag, 'xx', 1);
  await asNewVisitor(page.context());

  // Le sélecteur propose la langue du fichier plat, avec le nom de ses métadonnées.
  await page.goto('/');
  const select = page.locator('footer .lang-selector-select');
  await expect(select.locator('option[value="xx"]')).toHaveText('Testlandia');
  await expect(select.locator('option')).toContainText(['English', 'Français', 'Testlandia']);

  // Le choix pose le cookie et traduit les pages.
  await select.selectOption('xx');
  await page.locator('footer .lang-selector-apply').click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'xx');
  expect(await page.locator('body').innerText()).toContain('[xx] Skip to content');
  expect((await page.context().cookies()).find((c) => c.name === 'lang')?.value).toBe('xx');
  expect(await page.locator('main a.button').first().innerText()).toMatch(/^\[xx\] /);
  await page.locator('main a.button').first().click();
  await expect(page).toHaveURL(/\/creer$/);
  expect(await page.locator('main').innerText(), 'formulaire de création traduit').toContain('[xx] ');
  await page.goto(g.invitePath);
  expect(await page.locator('main').innerText(), 'formulaire d\'inscription traduit').toContain('[xx] ');
  await expectNoHorizontalScroll(page);

  // Le courriel de vérification part dans la langue choisie (lu dans le serveur SMTP factice).
  const email = `hector.${g.slug}@exemple.test`;
  await page.locator('input[name="prenom"]').fill('Hector');
  await page.locator('input[name="nom"]').fill('Parcours');
  await page.locator('input[name="email"]').fill(email);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('input[name="code"]')).toBeVisible();
  const mail = await waitForMailWhere((m) => m.To.some((t) => t.Address === email) && m.Subject.startsWith('[xx] '));
  expect(mail.Subject).toBe('[xx] Your verification code');
  expect(mail.Text).toContain('[xx] ');
  const code = extractVerificationCode(mail);
  expect(code, 'code de vérification').not.toBeNull();
  await page.locator('input[name="code"]').fill(code!);
  await page.locator('form.reg-card button[type="submit"]').click();
  await expect(page.locator('input[name="code"]')).toHaveCount(0);
  await expect(page.locator('html')).toHaveAttribute('lang', 'xx');
});
