import { defineConfig, devices } from "@playwright/test"

/**
 * Browser tests for Portal.
 *
 * These deliberately do not re-test the backend flow. `./make kind smoke`
 * already drives login, space, upload, conversation, task, worker, and artifact
 * through the API, and repeating that through a browser would be slower,
 * flakier, and no more informative. What only a browser can tell us is whether
 * the Portal bundle works against a real server: the runtime API base, routing,
 * session restoration, and the views that exist only in the UI.
 *
 * The stack has to be running already — see `./make e2e`, which also decides
 * where the run's artifacts land.
 */
export default defineConfig({
  testDir: "./e2e",
  // One worker: the tests share a signed-in session and read a single
  // deployment's state, so parallelism buys nothing and invites interference.
  workers: 1,
  fullyParallel: false,
  // A failing assertion here means the Portal is broken against a real server.
  // Retrying would only hide a flake that is worth seeing.
  retries: 0,
  timeout: 30_000,
  reporter: process.env.CI ? [["github"], ["list"]] : [["list"]],
  globalSetup: "./e2e/global-setup.ts",
  // One predictable directory per run, chosen by `./make e2e` and cleared
  // before it starts, so a failure's evidence is never mixed with an older
  // run's. Bare `npx playwright test` keeps Playwright's own default.
  outputDir: process.env.BUILDMAX_E2E_ARTIFACTS ?? "./test-results",
  use: {
    baseURL: process.env.BUILDMAX_E2E_BASE_URL ?? "http://localhost:8080",
    storageState: "./e2e/.auth/state.json",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    // The interface follows the browser language; specs assert English text.
    locale: "en-US",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
})
