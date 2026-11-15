import { expect, test } from '@playwright/test';
import { expectCleanAdminPage, open, trackDialogs } from '../../_shared/admin';
import { expectNoHorizontalScroll } from '../../_shared/mobile';
import { loginAdmin } from '../../_shared/seed';
import { MAIL_API, waitForMail } from '../../mail-sink';
import { createGroupWithPeople, uniqueTag, withMailLock } from './support';

// Parcours 08 (F12b.AC14) : délivrabilité, dans le conteneur.
//  1. la page de diagnostic affiche ses points à corriger et sa section « non vérifiable ici » ;
//  2. le courriel de test s'envoie et montre ses en-têtes ;
//  3. une adresse refusée par le serveur SMTP factice (code 550) entre dans la liste de suppression.
//
// Le refus passe par l'API de chaos de Mailpit (PUT /api/v1/chaos, activée par
// MP_ENABLE_CHAOS=true dans docker-compose.test.yml). Elle est globale : elle ne
// reste active que le temps d'un envoi, sous un verrou partagé entre les deux
// projets Playwright, puis elle est désactivée quoi qu'il arrive.

async function chaos(recipientProbability: number): Promise<void> {
  const res = await fetch(`${MAIL_API}/api/v1/chaos`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ Recipient: { ErrorCode: 550, Probability: recipientProbability } }),
  });
  if (!res.ok) {
    throw new Error(
      `API de chaos du serveur SMTP factice indisponible (HTTP ${res.status}) : ` +
        'MP_ENABLE_CHAOS=true manque au service mailpit de docker-compose.test.yml',
    );
  }
}

test('parcours 08 : diagnostic, courriel de test et liste de suppression', async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  const dialogs = trackDialogs(page);
  const tag = uniqueTag(testInfo.project.name);
  const { group: g } = await createGroupWithPeople(tag, 'delivr', 2);
  const accepted = `accepte.${g.slug}@exemple.test`;
  const refused = `refuse.${g.slug}@exemple.test`;

  await loginAdmin(page, g);
  await open(page, `/${g.slug}/admin/courrier/delivrabilite`);

  // 1. Diagnostic : points à corriger, section « non vérifiable ici ».
  await expect(page.locator('#maildiag-title')).toBeVisible();
  await expect(page.locator('#maildiag-report')).toBeVisible();
  const checks = page.locator('.maildiag-check');
  expect(await checks.count(), 'points de contrôle affichés').toBeGreaterThan(0);
  const fix = page.locator('.maildiag-chips .chip-error');
  await expect(fix).toBeVisible();
  expect(Number(/\d+/.exec((await fix.textContent()) ?? '')?.[0]), 'au moins un point à corriger').toBeGreaterThan(0);
  expect(await page.locator('.maildiag-check .chip-error').count()).toBeGreaterThan(0);
  const unverifiable = page.locator('#maildiag-unverifiable');
  await expect(unverifiable).toBeVisible();
  expect(await page.locator('.maildiag-unverifiable li').count(), 'éléments non vérifiables ici').toBeGreaterThan(0);
  await expect(page.locator('#maildiag-suppression-title')).toBeVisible();
  await expect(page.locator('.maildiag-suppressed-item')).toHaveCount(0);
  await expectCleanAdminPage(page, dialogs);

  // 2. Courriel de test accepté : reçu par le serveur factice, en-têtes affichés.
  const form = page.locator('form[action$="/delivrabilite/test"]');
  await withMailLock(async () => {
    await chaos(0);
    await form.locator('input[name="to"]').fill(accepted);
    await form.locator('button[type="submit"]').click();
    await expect(page.locator('section[aria-labelledby="maildiag-test"] p.maildiag-status[role="status"]')).toBeVisible();
  });
  const mail = await waitForMail(accepted);
  expect(mail.Subject).not.toBe('');
  const headers = page.locator('#maildiag-headers');
  await expect(headers).toBeVisible();
  const text = (await headers.textContent()) ?? '';
  for (const name of ['From:', 'To:', 'Subject:', 'Date:', 'Message-ID:', 'MIME-Version:']) {
    expect(text, `en-tête ${name}`).toContain(name);
  }
  expect(text).toContain(accepted);
  await expectCleanAdminPage(page, dialogs);

  // 3. Adresse refusée (550) : l'échec s'affiche, puis l'adresse figure dans la liste de suppression.
  await withMailLock(async () => {
    await chaos(100);
    try {
      await form.locator('input[name="to"]').fill(refused);
      await form.locator('button[type="submit"]').click();
      await expect(page.locator('section[aria-labelledby="maildiag-test"] p.field-error[role="alert"]')).toBeVisible();
    } finally {
      await chaos(0);
    }
  });
  await open(page, `/${g.slug}/admin/courrier/delivrabilite`);
  const suppressed = page.locator('.maildiag-suppressed-item');
  await expect(suppressed).toHaveCount(1);
  await expect(suppressed.first()).toContainText(refused);
  await expect(page.locator('.maildiag-suppressed-item', { hasText: accepted })).toHaveCount(0);
  await expect(suppressed.first().locator('form button[type="submit"]')).toBeVisible();
  await expectCleanAdminPage(page, dialogs);
  await expectNoHorizontalScroll(page);
  expect(dialogs()).toBe(0);
});
