import { createHmac } from 'node:crypto';
import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';

// Pre-requis : l'application tourne avec SIGNING_SECRET (le meme dans
// l'environnement du test) et un groupe existe ; son identifiant est dans
// E2E_GROUP_ID (defaut 1). Route attendue (internal/mailunsub) :
//   GET|POST /desinscription/{jeton}
// Le jeton est signe ici comme le fait mailunsub.Signer : cle derivee par
// HMAC-SHA-256 du domaine, puis HMAC de « v1|groupe|participant|type ».
const secret = process.env.SIGNING_SECRET ?? '';
const groupId = process.env.E2E_GROUP_ID ?? '1';
const b64 = (b: Buffer) => b.toString('base64url');

function link(type: string, participant: string): string {
  const key = createHmac('sha256', secret).update('secretsanta/mailunsub/v1').digest();
  const payload = `v1|${groupId}|${participant}|${type}`;
  const sig = createHmac('sha256', key).update(payload).digest();
  return `/desinscription/${b64(Buffer.from(payload))}.${b64(sig)}`;
}

test.skip(secret === '', 'SIGNING_SECRET requis pour signer le lien de test');

for (const type of ['relances', 'nouvelle_idee']) {
  test(`screen-desinscription (${type}) : confirmation, coupure puis reabonnement sur mobile`, async ({ page }) => {
    const path = link(type, `9${type.length}`);
    const res = await page.goto(path);
    expect(res?.status()).toBe(200);
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    // Le simple chargement ne coupe rien : pas de message d'etat.
    await expect(page.locator('[role="status"]')).toHaveCount(0);
    await page.locator('.unsub-form button[type="submit"]').click();
    await expect(page.locator('[role="status"]')).toBeVisible();
    await expectNoHorizontalScroll(page);
    await expectAllInteractiveTargets(page);
    // Le rechargement garde l'etat coupe et propose de se reabonner.
    await page.reload();
    await expect(page.locator('[role="status"]')).toBeVisible();
    await page.locator('.unsub-form button[type="submit"]').click();
    await expect(page.locator('.unsub-btn').first()).toBeVisible();
    await expectNoHorizontalScroll(page);
  });
}

test('screen-desinscription : bandeau vert pleine largeur et aucune boite du navigateur', async ({ page }) => {
  page.on('dialog', () => { throw new Error('boite du navigateur interdite'); });
  await page.goto(link('relances', '95'));
  const head = await page.locator('.unsub-head').boundingBox();
  const vw = page.viewportSize()!.width;
  expect(head!.x).toBeLessThanOrEqual(0.5);
  expect(head!.width).toBeGreaterThanOrEqual(vw - 0.5);
});

test('un lien a la signature falsifiee renvoie 404', async ({ page }) => {
  const good = link('relances', '96');
  const res = await page.goto(good.slice(0, -3) + 'AAA');
  expect(res?.status()).toBe(404);
});
