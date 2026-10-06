import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import AxeBuilder from '@axe-core/playwright'
import { expect } from '@playwright/test'

// The screenshot suite's stand-in for the Wails bridge.
//
// The Desktop UI suite proves the React app against the real bridge through
// `wails dev`, which only runs where the native toolchain does (macOS in CI).
// Appearance does not depend on the bridge, and baselines rendered on one
// Linux image are the only ones that compare reliably, so this suite serves
// the production build and answers each bound method from fixtures instead.

const here = path.dirname(fileURLToPath(import.meta.url))
// vite.config.js builds one level up, where desktop/assets_embed.go embeds it.
const dist = path.resolve(here, '..', '..', 'dist')

export const ORIGIN = 'http://desktop.visual'
export const NOW = new Date('2026-03-04T15:30:00Z')

const contentTypes = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
}

// Serve the bundle with a bridge whose methods resolve to `bridge[name]`.
// Returns the names of methods the app called that no fixture answers; each
// test asserts the list is empty, so a new call fails by name rather than as
// a screenshot of whatever an unanswered call rendered.
export async function serve(page, theme, bridge) {
  if (!fs.existsSync(path.join(dist, 'index.html'))) {
    throw new Error(`no Desktop build in ${dist}: run \`./make e2e visual\`, which builds it first`)
  }
  const unanswered = []
  await page.exposeFunction('__visualUnanswered', (name) => unanswered.push(name))
  await page.addInitScript(
    ({ theme, bridge }) => {
      localStorage.setItem('buildmax_theme', theme)
      const App = new Proxy(
        {},
        {
          get(_, name) {
            if (typeof name !== 'string' || name === 'then') return undefined
            return async () => {
              if (name in bridge) return structuredClone(bridge[name])
              window.__visualUnanswered(name)
              return null
            }
          },
        },
      )
      window.go = { desktop: { App } }
      window.runtime = { EventsOn: () => () => {}, EventsOff: () => {}, EventsEmit: () => {} }
    },
    { theme, bridge },
  )
  await page.clock.setFixedTime(NOW)
  await page.route(`${ORIGIN}/**`, async (route) => {
    const url = new URL(route.request().url())
    const file = path.join(dist, decodeURIComponent(url.pathname))
    if (file.startsWith(dist) && fs.existsSync(file) && fs.statSync(file).isFile()) {
      return route.fulfill({ path: file, contentType: contentTypes[path.extname(file)] })
    }
    return route.fulfill({ path: path.join(dist, 'index.html'), contentType: 'text/html' })
  })
  return unanswered
}

export async function settle(page) {
  await page.waitForLoadState('networkidle')
  await page.evaluate(() => document.fonts.ready)
  await expect(page.getByText('Loading…')).toHaveCount(0)
}

export async function expectAccessible(page) {
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
  const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
  const summary = results.violations
    .map((v) => `${v.id} (${v.help}): ${v.nodes.map((n) => `${n.target.join(' ')} ${n.failureSummary ?? ''}`).join('; ')}`)
    .join('\n')
  expect(results.violations, summary).toEqual([])
}
