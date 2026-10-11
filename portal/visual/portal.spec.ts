import { expect, test, type Page } from "@playwright/test"

import { fixtures, SPACE } from "./fixtures"
import { expectAccessible, ORIGIN, serve, settle, type Theme } from "./harness"

/**
 * What Portal looks like: the specimen and one page per template, in both
 * themes, at the wide and narrow widths the responsive record names. A
 * difference from the committed baseline fails until a reviewer accepts it by
 * refreshing the baselines (`./make e2e visual --update`) and the diff is
 * reviewed with the change that caused it.
 *
 * Each rendered view is also scanned by axe with contrast on. The specimen
 * carries every token and status tone, so it is what holds the theme itself to
 * WCAG AA; the deployed suite's accessibility.spec.ts covers live dialogs.
 */

const themes: Theme[] = ["light", "dark"]
const widths = [
  { name: "wide", viewport: { width: 1280, height: 900 } },
  { name: "narrow", viewport: { width: 390, height: 844 } },
]

interface View {
  name: string
  path: string
  /** What is on screen only once the page has its data. */
  ready: (page: Page) => Promise<void>
}

const space = `/#/spaces/${SPACE}`

const views: View[] = [
  {
    name: "specimen",
    path: "/specimen",
    ready: async (page) => {
      await expect(page.getByRole("heading", { level: 1, name: "Portal component specimen" })).toBeVisible()
    },
  },
  {
    name: "collection",
    path: `${space}/issues`,
    ready: async (page) => {
      await expect(page.getByText("Rotate the staging object-storage key")).toBeVisible()
    },
  },
  {
    name: "detail",
    path: `${space}/issues/iss_1`,
    ready: async (page) => {
      await expect(page.getByRole("heading", { level: 1, name: "Write the 0.3 release notes" })).toBeVisible()
      await expect(page.getByText("Collect merged pull requests")).toBeVisible()
    },
  },
  {
    // A draft Workflow opens on its Definition tab: the step editor.
    name: "editor",
    path: `${space}/workflows/wfl_release`,
    ready: async (page) => {
      await expect(page.getByRole("tab", { name: "Definition" })).toHaveAttribute("aria-selected", "true")
      await expect(page.locator(".react-flow__node").first()).toBeVisible()
    },
  },
  {
    // The work/run detail: a failed run, its retry, and the result.
    name: "diagnostics",
    path: `${space}/tasks/tsk_notes`,
    ready: async (page) => {
      await expect(page.getByRole("heading", { level: 1, name: "Draft the 0.3 release note" })).toBeVisible()
      // The failure in the person's terms; the server's text is under its disclosure.
      await expect(page.getByText("The model failed this run")).toBeVisible()
    },
  },
  {
    name: "settings",
    path: `${space}/settings`,
    ready: async (page) => {
      await expect(page.getByText("1,284,000")).toBeVisible()
    },
  },
]

for (const theme of themes) {
  for (const { name: width, viewport } of widths) {
    test.describe(`${theme} ${width}`, () => {
      test.use({ viewport })

      for (const view of views) {
        test(view.name, async ({ page }) => {
          const unanswered = await serve(page, theme, fixtures)
          await page.goto(`${ORIGIN}${view.path}`)
          await view.ready(page)
          await settle(page)
          await expect(page).toHaveScreenshot(`${view.name}-${theme}-${width}.png`, { fullPage: true })
          await expectAccessible(page)
          expect(unanswered).toEqual([])
        })
      }
    })
  }
}
