import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  timeout: 120_000,
  expect: { timeout: 15_000 },
  workers: 1,
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: [
    ['list'],
    ['json', { outputFile: process.env.PLAYWRIGHT_JSON_OUTPUT_FILE || '.local/browser-results.json' }],
  ],
  use: {
    baseURL: process.env.WALLETLEDGER_BASE_URL || 'http://127.0.0.1:8171',
    browserName: 'chromium',
    channel: process.env.PLAYWRIGHT_CHANNEL || (process.env.CI ? undefined : 'msedge'),
    viewport: { width: 1440, height: 1000 },
    actionTimeout: 15_000,
    navigationTimeout: 45_000,
    locale: 'id-ID',
    timezoneId: 'Asia/Jakarta',
    trace: 'off',
    video: 'off',
    screenshot: 'only-on-failure',
  },
});
