import { defineConfig } from '@playwright/test';

// Deux projets mobiles : 360x640 et 390x844.
// L'application est lancee separement ; son adresse vient de E2E_BASE_URL.
const baseURL = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080';

export default defineConfig({
  testDir: '.',
  testMatch: ['foundation/**/*.spec.ts', '**/*.spec.ts'],
  fullyParallel: true,
  globalSetup: './global-setup.ts',
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  // Garde du banc : aucun test sauté (reporter), rapport JSON recoupé par
  // scripts/e2e.sh avec _shared/check-report.mts.
  reporter: [
    [process.env.CI ? 'github' : 'list'],
    ['json', { outputFile: process.env.E2E_REPORT_FILE ?? 'test-results/report.json' }],
    ['./_shared/no-skip-reporter.ts'],
  ],
  use: {
    baseURL,
    locale: 'fr-FR',
    hasTouch: true,
    isMobile: true,
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'mobile-360x640', use: { viewport: { width: 360, height: 640 } } },
    { name: 'mobile-390x844', use: { viewport: { width: 390, height: 844 } } },
  ],
});
