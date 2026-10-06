import AxeBuilder from '@axe-core/playwright'
import { test, expect } from '@playwright/test'

// WCAG A/AA, contrast included, on the views a fresh sandbox reaches through
// the real bridge. The visual suite checks the same views from fixtures on
// every pull request; this confirms the live app, whose data the bridge
// supplies, renders nothing the fixtures missed.

async function expectAccessible(page) {
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
    .map((v) => `${v.id} (${v.help}): ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`)
    .join('\n')
  expect(results.violations, summary).toEqual([])
}

for (const theme of ['light', 'dark']) {
  test(`home and the New Project dialog have no WCAG A/AA violations (${theme})`, async ({ page }) => {
    await page.addInitScript((value) => localStorage.setItem('buildmax_theme', value), theme)
    await page.goto('/')
    await expect(page.getByText('Loading…')).toHaveCount(0, { timeout: 15_000 })
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
    await expectAccessible(page)

    await page.locator('.page-home__primary').click()
    await expect(page.locator('.modal-panel')).toBeVisible()
    await expectAccessible(page)
  })
}
