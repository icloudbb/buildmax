import { test, expect } from '@playwright/test'

// The Schedules view in Chinese, reached the way a person gets there: choose
// the language from the user menu, then open Schedules from the sidebar. The
// switch applies in place, so no reload is involved. Opening the New Schedule
// form calls only bound Go methods that answer without a native dialog.
test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Loading…')).toHaveCount(0, { timeout: 15_000 })
})

test('the Schedules view renders in Chinese after switching the language', async ({ page }) => {
  await page.locator('.sidebar__user-trigger').click()
  await page.getByRole('menuitemradio', { name: '简体中文' }).click()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')

  await page.locator('.sidebar__row-label', { hasText: '定时任务' }).click()

  const view = page.locator('.page-schedules')
  await expect(view.locator('.page-schedules__title')).toHaveText('定时任务')
  await expect(view.getByText('定时任务仅在本应用打开时运行。')).toBeVisible()
  await expect(view.getByRole('heading', { name: '最近运行' })).toBeVisible()

  await view.getByRole('button', { name: '新建定时任务' }).click()
  const form = page.getByRole('dialog', { name: '新建定时任务' })
  await expect(form).toBeVisible()
  await expect(form.getByRole('button', { name: '创建定时任务' })).toBeVisible()
  await form.getByRole('button', { name: '取消' }).click()
  await expect(form).toBeHidden()
})
