import { defineConfig, devices } from '@playwright/test'

// Browser tests for the Desktop React app.
//
// These run against `wails dev`'s own dev server (default
// http://localhost:34115), not the packaged/signed app: that server injects
// the real Wails runtime and Go bindings into a plain page, so a normal
// browser exercises the actual bridge without a native window in the loop.
// It proves the React app and its bound methods work together; it says
// nothing about how the native window renders the same page. `./make e2e
// desktop-ui` starts the dev server, waits for it, and stops it afterward —
// see tools/mk/desktop_ui.go.
export default defineConfig({
  testDir: './e2e',
  // One worker: every spec drives the same `wails dev` instance and its one
  // isolated BUILDMAX_HOME, so parallelism buys nothing.
  workers: 1,
  fullyParallel: false,
  // A failing assertion here means the bridge or the app broke. Retrying
  // would only hide a flake worth seeing.
  retries: 0,
  timeout: 30_000,
  reporter: process.env.CI ? [['github'], ['list']] : [['list']],
  // One predictable directory per run, chosen by `./make e2e desktop-ui` and
  // cleared before it starts. Bare `npx playwright test` keeps Playwright's
  // own default.
  outputDir: process.env.BUILDMAX_E2E_ARTIFACTS ?? './test-results',
  use: {
    baseURL: process.env.BUILDMAX_E2E_BASE_URL ?? 'http://localhost:34115',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    // The interface follows the browser language; specs assert English text.
    locale: 'en-US',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
