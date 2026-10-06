import { defineConfig, devices } from "@playwright/test"

/**
 * Screenshot comparisons for Portal's look, plus an axe pass with contrast.
 *
 * Unlike `playwright.config.ts`, nothing here needs a deployment: the specs
 * serve the production build and answer the API from fixtures (see
 * visual/harness.ts), so the suite can gate pull requests. Run it through
 * `./make e2e visual`, which renders inside the Playwright image that made the
 * committed baselines. Fonts differ across platforms, so a bare run on macOS
 * compares against nothing (the baselines are named for linux) and is not
 * evidence either way.
 */
export default defineConfig({
  testDir: "./visual",
  // Every test owns its page and its fixtures, so they can run side by side.
  fullyParallel: true,
  retries: 0,
  // A full-page capture of the specimen is several thousand pixels tall and
  // is taken twice to prove the page stable; on a busy runner that outlasts
  // Playwright's defaults.
  timeout: 60_000,
  reporter: process.env.CI ? [["github"], ["list"]] : [["list"]],
  outputDir: process.env.BUILDMAX_E2E_ARTIFACTS ?? "./test-results/visual",
  snapshotPathTemplate: "{testDir}/__screenshots__/{testFilePath}/{arg}-{platform}{ext}",
  expect: {
    timeout: 15_000,
    toHaveScreenshot: {
      animations: "disabled",
      caret: "hide",
      // One image renders every baseline, so the output is reproducible and
      // the comparison can be strict. Playwright's default per-pixel threshold
      // (0.2) would let a text token move by about thirty grey levels unseen,
      // which is exactly the change a contrast fix makes.
      threshold: 0.02,
      maxDiffPixels: 0,
    },
  },
  use: {
    trace: "retain-on-failure",
    locale: "en-US",
    timezoneId: "UTC",
    reducedMotion: "reduce",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
})
