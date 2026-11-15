import { expect, test } from '@playwright/test';
import { expectAllInteractiveTargets, expectNoHorizontalScroll } from '../_shared/mobile';
import { lane, unsubscribeLink } from '../_shared/seed';

// Route (internal/mailunsub) : GET|POST /desinscription/{jeton}. Les liens sont
// signes par _shared/seed.ts (unsubscribeLink, meme cle que mailunsub.Signer) pour
// de vraies personnes du groupe d'amorcage « people » du couloir, avec le vrai
// numero de groupe : plus de variables SIGNING_SECRET ni E2E_GROUP_ID a fournir.
const TYPES = ['relances', 'nouvelle_idee'] as const;

for (const type of TYPES) {
  test(`screen-desinscription (${type}) : confirmation, coupure puis reabonnement sur mobile`, async ({ page }, testInfo) => {
    const g = lane(testInfo).people;
    const path = await unsubscribeLink(g, g.people[TYPES.indexOf(type)], type);
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

test('screen-desinscription : bandeau vert pleine largeur et aucune boite du navigateur', async ({ page }, testInfo) => {
  page.on('dialog', () => { throw new Error('boite du navigateur interdite'); });
  const g = lane(testInfo).people;
  await page.goto(await unsubscribeLink(g, g.people[2], 'relances'));
  const head = await page.locator('.unsub-head').boundingBox();
  const vw = page.viewportSize()!.width;
  expect(head!.x).toBeLessThanOrEqual(0.5);
  expect(head!.width).toBeGreaterThanOrEqual(vw - 0.5);
});

test('un lien a la signature falsifiee renvoie 404', async ({ page }, testInfo) => {
  const g = lane(testInfo).people;
  const good = await unsubscribeLink(g, g.people[3], 'relances');
  const res = await page.goto(good.slice(0, -3) + 'AAA');
  expect(res?.status()).toBe(404);
});
