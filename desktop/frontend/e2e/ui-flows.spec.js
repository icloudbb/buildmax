import { test, expect } from '@playwright/test'

// Two more deterministic, credential-free checks alongside golden-path.spec.js.
// Both stay inside the React app's own state — no bound Go method that could
// hang on a native dialog (OpenFolderDialog opens a real OS picker Playwright
// cannot drive), no login.
test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Loading…')).toHaveCount(0, { timeout: 15_000 })
})

test('opens and closes the New Project modal', async ({ page }) => {
  // Not getByRole('button', { name: 'New Project' }): the sidebar has its own
  // "New Project" entry point (icon-only when the list is non-empty, a
  // labeled button in the empty state this fresh sandbox always shows), so
  // that name is not unique. This is the "Continue your work" header button.
  await page.locator('.page-home__primary').click()
  const modal = page.locator('.modal-panel')
  await expect(modal).toBeVisible()
  await expect(modal.getByRole('heading', { name: 'New Project' })).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(modal).toBeHidden()
})

test('toggles the theme from the status bar', async ({ page }) => {
  const initial = await page.evaluate(() => document.documentElement.getAttribute('data-theme'))

  // The theme toggle is a global status-bar control — present even on Home and
  // pinned at the far right — not a user-menu entry. On Home it is the only
  // status-bar button, so its "…mode" aria-label pins it unambiguously.
  const themeBtn = page.locator('.workspace-statusbar__btn[aria-label$="mode"]')
  await expect(themeBtn).toBeVisible()
  await themeBtn.click()

  await expect
    .poll(() => page.evaluate(() => document.documentElement.getAttribute('data-theme')))
    .not.toBe(initial)
})

test('the New Project form keeps Create disabled until a folder is chosen', async ({ page }) => {
  await page.locator('.page-home__primary').click()
  const modal = page.locator('.modal-panel')
  await expect(modal).toBeVisible()

  // A name alone is not enough: the folder comes only from the native picker
  // (OpenFolderDialog), which this suite never opens, so Create stays disabled.
  // fill, not a keypress: pressing Enter with an empty name opens that picker.
  const create = modal.locator('.modal-btn--primary')
  await expect(create).toBeDisabled()
  await modal.locator('#proj-name').fill('probe project')
  await expect(create).toBeDisabled()

  // The × closes it, a mouse path the Escape test does not cover.
  await modal.locator('.modal-close').click()
  await expect(modal).toBeHidden()
})

test('the home page shows its empty-state guidance in a fresh sandbox', async ({ page }) => {
  await expect(page.locator('.page-home__title')).toHaveText('Continue your work')
  await expect(page.getByRole('heading', { name: 'Recent chats' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Recent projects' })).toBeVisible()
  // No account, no history: both collections render their empty copy, not rows.
  await expect(page.getByText('No recent chats yet.')).toBeVisible()
})

test('collapses and re-expands the sidebar', async ({ page }) => {
  const shell = page.locator('.shell')
  await expect(shell).not.toHaveClass(/shell--left-collapsed/)

  // One status-bar control both hides and shows the sidebar, so it stays
  // reachable while the sidebar is gone.
  await page.getByRole('button', { name: 'Hide sidebar' }).click()
  await expect(shell).toHaveClass(/shell--left-collapsed/)

  await page.getByRole('button', { name: 'Show sidebar' }).click()
  await expect(shell).not.toHaveClass(/shell--left-collapsed/)
})

test('opening the server sign-in and cancelling returns to local mode', async ({ page }) => {
  await page.locator('.sidebar__user-trigger').click()
  await page.getByText('Sign in to a server', { exact: true }).click()

  const login = page.locator('.login-page')
  await expect(login).toBeVisible()
  // Sign in stays disabled with no credentials, and the submit is never clicked
  // here: it calls the Go Login binding. Cancel is pure in-app state.
  await expect(login.locator('.login-page__submit')).toBeDisabled()

  await login.locator('.login-page__local').click()
  await expect(login).toBeHidden()
  await expect(page.locator('.page-home__title')).toHaveText('Continue your work')
})
