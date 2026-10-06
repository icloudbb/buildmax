import { defineConfig, devices } from '@playwright/test'

// Screenshot comparisons for the Desktop app's look, plus an axe pass with
// contrast. Unlike playwright.config.js this needs no `wails dev`: the specs
// serve the production build with a fixture bridge (see visual/harness.js).
// Run it through `./make e2e visual`, which renders inside the Playwright
// image that made the committed baselines; they are named for linux, so a
// bare run elsewhere compares against nothing and is not evidence.
export default defineConfig({
  testDir: './visual',
  fullyParallel: true,
  retries: 0,
  // Timeouts and strictness for the reasons portal/playwright.visual.config.ts gives.
  timeout: 60_000,
  reporter: process.env.CI ? [['github'], ['list']] : [['list']],
  outputDir: process.env.BUILDMAX_E2E_ARTIFACTS ?? './test-results/visual',
  snapshotPathTemplate: '{testDir}/__screenshots__/{testFilePath}/{arg}-{platform}{ext}',
  expect: {
    timeout: 15_000,
    toHaveScreenshot: { animations: 'disabled', caret: 'hide', threshold: 0.02, maxDiffPixels: 0 },
  },
  use: {
    trace: 'retain-on-failure',
    locale: 'en-US',
    timezoneId: 'UTC',
    reducedMotion: 'reduce',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
