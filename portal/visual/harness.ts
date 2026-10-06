import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

import AxeBuilder from "@axe-core/playwright"
import { expect, type Page } from "@playwright/test"

/**
 * The screenshot suite's stand-in for a deployment.
 *
 * Every request the page makes is answered here: the production bundle from
 * `dist/`, and the API from fixtures. Nothing listens on a port and no server
 * runs, which is what lets the suite gate pull requests: a deployment is the
 * one thing a pull-request job cannot cheaply have, and appearance does not
 * depend on one.
 */

const here = path.dirname(fileURLToPath(import.meta.url))
const dist = path.resolve(here, "..", "dist")

/** The origin the bundle is served from. It never resolves in DNS. */
export const ORIGIN = "http://portal.visual"

/** The instant every page is rendered at, so relative times never drift. */
export const NOW = new Date("2026-03-04T15:30:00Z")

export type Theme = "light" | "dark"

/** One fixture answer: a JSON body, or a status with one. */
export type Answer = unknown | { status: number; body: unknown }

/**
 * API answers keyed by `METHOD /path`, the path without its query. A function
 * value receives the full URL for the few endpoints that answer by query.
 */
export type Fixtures = Record<string, Answer | ((url: URL) => Answer)>

const contentTypes: Record<string, string> = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".json": "application/json",
  ".md": "text/markdown",
  ".woff2": "font/woff2",
}

/**
 * Serve the bundle and the fixtures, and pin everything that would otherwise
 * make two renders differ: the theme, the clock, and the socket.
 *
 * It returns the API requests no fixture answered. A page that asks for
 * something unanswered renders an error state the reviewer did not choose, so
 * each test asserts the list is empty: a new request the page starts making
 * fails here, by name, instead of as a puzzling screenshot diff.
 */
export async function serve(page: Page, theme: Theme, fixtures: Fixtures): Promise<string[]> {
  if (!fs.existsSync(path.join(dist, "index.html"))) {
    throw new Error(`no Portal build in ${dist}: run \`./make e2e visual\`, which builds it first`)
  }
  const unanswered: string[] = []

  await page.addInitScript((value) => {
    localStorage.setItem("buildmax_theme", value)
  }, theme)
  // Fixed, not frozen: timers keep running so the app's own effects settle,
  // while every "3 hours ago" is computed from the same instant.
  await page.clock.setFixedTime(NOW)

  // The space socket would otherwise retry forever against a host that does
  // not exist. Accepting it and saying nothing is a healthy, idle space.
  await page.routeWebSocket(/\/ws(\?|$)/, () => {})

  await page.route(`${ORIGIN}/**`, async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/config.js") {
      // Same-origin API, so the fixtures below answer it.
      return route.fulfill({ contentType: "text/javascript", body: 'window.__BUILDMAX_CONFIG__ = { apiBase: "/" }' })
    }
    if (url.pathname.startsWith("/api/")) {
      const key = `${request.method()} ${url.pathname}`
      if (!(key in fixtures)) {
        unanswered.push(key)
        return route.fulfill({ status: 404, json: { error: { code: "not_found", message: `no fixture for ${key}` } } })
      }
      const entry = fixtures[key]
      const answer = typeof entry === "function" ? entry(url) : entry
      if (isStatusAnswer(answer)) return route.fulfill({ status: answer.status, json: answer.body })
      return route.fulfill({ json: answer })
    }
    const file = path.join(dist, decodeURIComponent(url.pathname))
    if (file.startsWith(dist) && fs.existsSync(file) && fs.statSync(file).isFile()) {
      return route.fulfill({ path: file, contentType: contentTypes[path.extname(file)] })
    }
    // Every other path is the single-page app.
    return route.fulfill({ path: path.join(dist, "index.html"), contentType: "text/html" })
  })
  return unanswered
}

function isStatusAnswer(answer: Answer): answer is { status: number; body: unknown } {
  return typeof answer === "object" && answer !== null && "status" in answer && "body" in answer
}

/**
 * Wait until the page has stopped asking for anything, so the screenshot shows
 * the settled view. Fonts too: a fallback face swapped in mid-capture is the
 * most common source of a one-pixel flake.
 */
export async function settle(page: Page): Promise<void> {
  await page.waitForLoadState("networkidle")
  await page.evaluate(() => document.fonts.ready)
}

/**
 * WCAG A/AA, contrast included, over the whole rendered page. A violation is a
 * defect to fix in the tokens or the component, never a rule to disable.
 */
export async function expectAccessible(page: Page): Promise<void> {
  // A dialog measured mid-fade reports its half-transparent frame, not the
  // colors anyone reads; infinite animations (spinners) are left running.
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze()
  const summary = results.violations
    .map((v) => `${v.id} (${v.help}): ${v.nodes.map((n) => `${n.target.join(" ")} ${n.failureSummary ?? ""}`).join("; ")}`)
    .join("\n")
  expect(results.violations, summary).toEqual([])
}
