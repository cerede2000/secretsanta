import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../_shared/admin';
import { Client, expectStatus, unescapeHTML } from '../_shared/http';
import { expectNoHorizontalScroll } from '../_shared/mobile';
import { loginAdmin, PROJECTS, seed, type Group } from '../_shared/seed';

// F10b.AC9 (part de la consultation de paire) : la page de consultation, son
// résultat et le journal des consultations n'ont aucun défilement horizontal
// et leurs zones tactiles mesurent au moins 44 px en 360x640 et 390x844.
// Les groupes d'amorçage n'ont pas l'option « l'admin peut consulter une
// paire » : la spec crée son propre groupe, option cochée, par les pages
// réelles (/creer, connexion, participants, tirage).

const PEOPLE = ['Agathe', 'Basile', 'Capucine', 'Désiré'];

async function groupWithOption(index: number): Promise<{ group: Group; secret: string }> {
  const s = seed();
  const admin = new Client(s.baseUrl, `198.51.100.${40 + index}`);
  const slug = `consult-${index}-${Date.now().toString(36)}`;
  const created = expectStatus(
    `création de ${slug}`,
    await admin.submit('/creer', '/creer', {
      password: s.instancePassword,
      name: `Consultation ${index}`,
      slug,
      budget: '',
      admin_can_view_pair: 'on',
    }),
    200,
  );
  const raw = /id="admin-secret">([^<]+)</.exec(created.body)?.[1];
  if (!raw) throw new Error(`secret administrateur de ${slug} introuvable`);
  const secret = unescapeHTML(raw).trim();
  expectStatus(
    `connexion de ${slug}`,
    await admin.submit(`/${slug}/admin/connexion`, `/${slug}/admin/connexion`, { secret }),
    303,
  );
  for (const first of PEOPLE) {
    expectStatus(
      `ajout de ${first}`,
      await admin.submit(`/${slug}/admin/participants`, `/${slug}/admin/participants`, {
        first_name: first,
        name: 'Consulte',
      }),
      200,
      303,
    );
  }
  expectStatus(
    `tirage de ${slug}`,
    await admin.submit(`/${slug}/admin/tirage?confirmer=lancer`, `/${slug}/admin/tirage/lancer`, { confirm: '1' }),
    303,
  );
  return {
    group: { slug, name: `Consultation ${index}`, budget: '', adminSecret: secret, invitePath: '', people: [], pending: [] },
    secret,
  };
}

test('consultation d\'une paire : formulaire, erreur sous le champ, résultat et journal sans défilement ni petite cible', async ({ page }, testInfo) => {
  const index = Math.max(0, PROJECTS.indexOf(testInfo.project.name));
  const { group, secret } = await groupWithOption(index);
  const dialogs = trackDialogs(page);
  await loginAdmin(page, group);

  // Formulaire : une personne à choisir, le mot de passe à ressaisir.
  await open(page, `/${group.slug}/admin/consultation`);
  const form = page.locator(`form[action="/${group.slug}/admin/consultation"]`);
  await expect(form.locator('select[name="person"]')).toBeVisible();
  await expect(form.locator('input[name="password"][type="password"]')).toBeVisible();
  await expect(page.locator('table')).toHaveCount(0);
  await expectCleanAdminPage(page, dialogs);

  // Mot de passe faux : message sous le champ, aucune paire.
  await form.locator('select[name="person"]').selectOption({ label: `${PEOPLE[0]} Consulte` });
  await form.locator('input[name="password"]').fill('pas-le-bon');
  await form.locator('button[type="submit"]').click();
  await expect(page.locator('#ac-password-error[role="alert"]')).toBeVisible();
  await expect(page.locator('#ac-password')).toHaveAttribute('aria-invalid', 'true');
  await expect(page.locator('.adminconsult-target')).toHaveCount(0);
  await expectCleanAdminPage(page, dialogs);

  // Bon mot de passe : la cible de cette seule personne.
  await page.locator('select[name="person"]').selectOption({ label: `${PEOPLE[0]} Consulte` });
  await page.locator('input[name="password"]').fill(secret);
  await page.locator(`form[action="/${group.slug}/admin/consultation"] button[type="submit"]`).click();
  await expect(page.locator('.adminconsult-giver')).toContainText(PEOPLE[0]);
  await expect(page.locator('.adminconsult-target')).toHaveCount(1);
  await expectCleanAdminPage(page, dialogs);

  // Journal : une ligne, aucun nom.
  await open(page, `/${group.slug}/consultations`);
  await expect(page.locator('.adminconsult-entry')).toHaveCount(1);
  for (const first of PEOPLE) await expect(page.locator('main')).not.toContainText(first);
  await expectNoHorizontalScroll(page);
  await expectCleanAdminPage(page, dialogs);
});
