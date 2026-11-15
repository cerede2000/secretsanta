import { expect, type Page } from '@playwright/test';

// Pre-requis (hors de ce depot) : un groupe dont le slug est dans E2E_DRAW_SLUG
// (defaut « e2e-draw »), avec des participants verifies dont les codes de
// connexion sont fournis par variables d'environnement. Routes : /<slug>/connexion
// puis /<slug>/participant (internal/targetpage, Home).
export const slug = process.env.E2E_DRAW_SLUG ?? 'e2e-draw';

export async function loginWithCode(page: Page, code: string): Promise<void> {
  await page.goto(`/${slug}/connexion`);
  await page.locator('#access-code').fill(code);
  await page.locator(`form[action="/${slug}/connexion/code"] button[type="submit"]`).click();
  await page.waitForURL((u) => !u.pathname.endsWith('/connexion') && !u.pathname.endsWith('/connexion/code'));
  const res = await page.goto(`/${slug}/participant`);
  expect(res?.status(), 'la page du participant doit exister').toBe(200);
}
