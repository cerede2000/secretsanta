import { expect, test } from '@playwright/test';
import { ADMIN_PATH, HOME_PATH } from './paths';

for (const [name, path] of [['accueil', HOME_PATH], ['admin', ADMIN_PATH]] as const) {
  test(`${name} : CSP stricte respectee`, async ({ page }) => {
    const problems: string[] = [];
    page.on('console', (m) => {
      if (/content security policy|refused to/i.test(m.text())) problems.push(m.text());
    });
    page.on('pageerror', (e) => problems.push(e.message));
    await page.addInitScript(() => {
      (window as any).__csp = [];
      document.addEventListener('securitypolicyviolation', (e) =>
        (window as any).__csp.push(`${e.violatedDirective} ${e.blockedURI}`),
      );
    });
    const res = await page.goto(path);
    expect(res?.headers()['content-security-policy'], 'en-tete CSP absent').toBeTruthy();
    await page.waitForLoadState('networkidle');

    expect(await page.locator('[style]').count(), 'attribut style en ligne').toBe(0);
    expect(await page.locator('style').count(), 'bloc <style>').toBe(0);
    const dataRefs = await page.evaluate(() =>
      Array.from(document.querySelectorAll('[src],[href],[srcset]'))
        .map((e) => (e.getAttribute('src') ?? '') + (e.getAttribute('href') ?? '') + (e.getAttribute('srcset') ?? ''))
        .filter((v) => v.trim().toLowerCase().startsWith('data:')),
    );
    expect(dataRefs, 'ressource data:').toEqual([]);
    expect(await page.evaluate(() => (window as any).__csp)).toEqual([]);
    expect(problems).toEqual([]);
  });
}
