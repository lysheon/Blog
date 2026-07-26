import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://127.0.0.1:4173'

// E2E_REAL_API=1 switches the suite from the browser-mocked smoke to the
// real-API flow, which expects a live backend behind the preview proxy
// (VITE_DEV_API_TARGET). The two modes never run in one invocation: the
// mocked specs intercept /api/v1 and would starve the real backend.
const realAPI = Boolean(process.env.E2E_REAL_API)

export default defineConfig({
  testDir: './e2e',
  testMatch: realAPI ? /real-api\.spec\.ts/ : /smoke\.spec\.ts/,
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  timeout: realAPI ? 90_000 : 30_000,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run preview -- --host 127.0.0.1 --port 4173',
    url: baseURL,
    // A leftover preview from a mocked run would lack the API relay target,
    // so the real-API mode always starts its own server.
    reuseExistingServer: !process.env.CI && !realAPI,
    timeout: 120_000,
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
})
