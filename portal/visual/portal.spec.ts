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
  /** The interface language, when the view is about its wording too. */
  locale?: "en" | "zh-CN"
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
    // The landing page: what waits on the person, then the collection.
    name: "collection",
    path: `${space}/issues`,
    ready: async (page) => {
      await expect(page.getByText("Document the sandbox network tiers")).toBeVisible()
      await expect(page.getByText("Approve the draft release note before it is published.")).toBeVisible()
    },
  },
  {
    name: "collection-zh",
    path: `${space}/issues`,
    locale: "zh-CN",
    ready: async (page) => {
      await expect(page.getByRole("heading", { level: 2, name: "等待你处理" })).toBeVisible()
      await expect(page.getByText("Document the sandbox network tiers")).toBeVisible()
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
      // A wide window shows the graph; a narrow one lists the steps instead,
      // under a note that connecting steps needs a wider window. The view
      // opens at that note so the list is what the narrow baseline shows.
      await expect(page.locator(".react-flow__node, .wf-step-list").first()).toBeVisible()
      const narrowNote = page.locator(".wf-visual__narrow-note")
      if ((await narrowNote.count()) > 0) {
        await narrowNote.evaluate((note) => note.scrollIntoView({ block: "start" }))
      }
    },
  },
  {
    // The work/run detail: a failed run, its retry, and the result.
    name: "diagnostics",
    path: `${space}/tasks/tsk_notes`,
    ready: async (page) => {
      // Named by the Issue it serves; its own generated title is secondary.
      await expect(page.getByRole("heading", { level: 1, name: "Write the 0.3 release notes" })).toBeVisible()
      // The failure in the person's terms; the server's text is under its disclosure.
      await expect(page.getByText("The model failed this run")).toBeVisible()
    },
  },
  // Administration Overview. The page scrolls inside the shell, so each view
  // opens at a different part: in English the failures, each with who acts and
  // what they check, and the Spaces needing attention with their Space link;
  // in Chinese the deployment facts and run statuses read as labels.
  {
    name: "administration",
    path: "/#/admin",
    ready: async (page) => {
      await expect(page.getByRole("link", { name: "Analytical Engine" })).toBeVisible()
      await page
        .getByRole("heading", { name: "Task run failures in the last 24 hours" })
        .evaluate((heading) => heading.scrollIntoView({ block: "start" }))
    },
  },
  {
    name: "administration-zh",
    path: "/#/admin",
    locale: "zh-CN",
    ready: async (page) => {
      await expect(page.getByText("对象存储", { exact: true })).toBeVisible()
      await page
        .getByRole("heading", { name: "部署", exact: true })
        .evaluate((heading) => heading.scrollIntoView({ block: "start" }))
    },
  },
  // A Space's Administration detail: its tier by name and its quota grouped.
  {
    name: "administration-space",
    path: `/#/admin/spaces/${SPACE}`,
    ready: async (page) => {
      await expect(page.getByText("258 / 1,000")).toBeVisible()
      await expect(page.getByText("1,284,000 / 10,000,000")).toBeVisible()
    },
  },
  {
    name: "settings",
    path: `${space}/settings`,
    ready: async (page) => {
      await expect(page.getByText("1,284,000")).toBeVisible()
    },
  },
  // Confirming a CLI sign-in, in both languages: the unverified-label marker
  // and the warning are the page's defense, so their wording is reviewed too.
  {
    name: "sign-in",
    path: "/#/sign-in/WDJB-MJHT",
    locale: "en",
    ready: async (page) => {
      await expect(page.getByText("Reported by the client, not verified")).toBeVisible()
      await expect(page.getByRole("button", { name: "Confirm sign-in" })).toBeVisible()
    },
  },
  // Account → Sessions, in both languages: the unverified-label marker and the
  // current-browser mark are what someone recognising a stray session reads.
  {
    name: "sessions",
    path: "/#/account/sessions",
    locale: "en",
    ready: async (page) => {
      await expect(page.getByText("ada-mbp · macOS")).toBeVisible()
      await expect(page.getByText("This browser", { exact: true })).toBeVisible()
    },
  },
  {
    name: "sessions-zh",
    path: "/#/account/sessions",
    locale: "zh-CN",
    ready: async (page) => {
      await expect(page.getByText("ada-mbp · macOS")).toBeVisible()
      await expect(page.getByText("当前浏览器")).toBeVisible()
    },
  },
  {
    name: "sign-in-zh",
    path: "/#/sign-in/WDJB-MJHT",
    locale: "zh-CN",
    ready: async (page) => {
      await expect(page.getByText("由客户端自行报告，未经验证")).toBeVisible()
      await expect(page.getByRole("button", { name: "确认登录" })).toBeVisible()
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
          if (view.locale) {
            await page.addInitScript((locale) => localStorage.setItem("buildmax_locale", locale), view.locale)
          }
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
