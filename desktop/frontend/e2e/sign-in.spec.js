import { test, expect } from '@playwright/test'

// The sign-in page laid out by a server's methods, and the panel that waits for
// a browser sign-in (design enterprise-identity-and-access §14.7).
//
// The React app and the Wails runtime are real; the sign-in bindings are
// replaced in the page. The real ones reach a server and open the system
// browser, and nothing in this suite may open a browser. The Go side of the
// same flow — the real routes, the browser call, the polling — is the Desktop
// bridge suite's (`./make e2e desktop`).

const SERVER = 'https://buildmax.corp.example'

test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('Loading…')).toHaveCount(0, { timeout: 15_000 })
})

// Stand in for the sign-in bindings: a configured server answering with
// `methods`, and a browser sign-in that starts with code WDJB-MJHT. Every call
// is recorded in window.__signInCalls.
async function stubServer(page, methods) {
  await page.evaluate(
    ({ server, methods }) => {
      const app = window.go.desktop.App
      const calls = []
      window.__signInCalls = calls
      app.GetDefaultServerURL = async () => server
      app.GetAuthMethods = async (url) => {
        calls.push(['GetAuthMethods', url])
        return methods
      }
      app.StartBrowserSignIn = async (url) => {
        calls.push(['StartBrowserSignIn', url])
        return {
          user_code: 'WDJB-MJHT',
          verification_uri: `${server}/#/sign-in`,
          verification_uri_complete: `${server}/#/sign-in/WDJB-MJHT`,
          expires_in: 600,
        }
      }
      for (const name of ['OpenBrowserSignInAgain', 'CopyBrowserSignInLink', 'CancelBrowserSignIn']) {
        app[name] = async () => {
          calls.push([name])
        }
      }
    },
    { server: SERVER, methods },
  )
}

async function openSignIn(page) {
  await page.locator('.sidebar__user-trigger').click()
  await page.getByText('Sign in to a server', { exact: true }).click()
  const login = page.locator('.login-page')
  await expect(login).toBeVisible()
  await expect(login.getByLabel('Server address')).toHaveValue(SERVER)
  return login
}

async function calls(page) {
  return page.evaluate(() => window.__signInCalls)
}

test('local_login all: the password form leads and the browser follows it', async ({ page }) => {
  await stubServer(page, { local_login: 'all', oidc: { enabled: false } })
  const login = await openSignIn(page)
  const browser = login.getByRole('button', { name: 'Sign in with your browser' })
  await expect(browser).toBeVisible()
  await expect(login.getByLabel('Password')).toBeVisible()
  const passwordTop = (await login.getByLabel('Password').boundingBox()).y
  expect((await browser.boundingBox()).y).toBeGreaterThan(passwordTop)
  expect(await calls(page)).toContainEqual(['GetAuthMethods', SERVER])
})

test('local_login system_admins: the browser leads and the password waits for an administrator', async ({ page }) => {
  await stubServer(page, { local_login: 'system_admins', oidc: { enabled: true, display_name: 'Okta' } })
  const login = await openSignIn(page)
  await expect(login.getByRole('button', { name: 'Continue with Okta in your browser' })).toBeVisible()
  await expect(login.getByLabel('Password')).toHaveCount(0)
  await login.getByRole('button', { name: 'Administrator sign-in with a password' }).click()
  await expect(login.getByLabel('Password')).toBeVisible()
})

test('local_login off: the browser is the only way in, and the panel waits for it', async ({ page }) => {
  await stubServer(page, { local_login: 'off', oidc: { enabled: true, display_name: 'Okta' } })
  const login = await openSignIn(page)
  await expect(login.getByLabel('Password')).toHaveCount(0)
  await expect(login.getByLabel('Email')).toHaveCount(0)

  await login.getByRole('button', { name: 'Continue with Okta in your browser' }).click()
  await expect(login.getByText('WDJB-MJHT', { exact: true })).toBeVisible()
  await expect(login.getByText('Confirm this sign-in in the browser window that opened. Check that it shows this code.')).toBeVisible()
  await login.getByRole('button', { name: 'Open browser again' }).click()
  await login.getByRole('button', { name: 'Copy link' }).click()
  await expect(login.getByText(/Link copied/)).toBeVisible()
  await login.getByRole('button', { name: 'Cancel' }).click()
  await expect(login.getByText('WDJB-MJHT', { exact: true })).toHaveCount(0)
  expect(await calls(page)).toEqual(
    expect.arrayContaining([
      ['StartBrowserSignIn', SERVER],
      ['OpenBrowserSignInAgain'],
      ['CopyBrowserSignInLink'],
      ['CancelBrowserSignIn'],
    ]),
  )
})

test('a denial and an expiry return to the page with their messages, in both languages', async ({ page }) => {
  await stubServer(page, { local_login: 'off', oidc: { enabled: false } })
  const login = await openSignIn(page)

  await login.getByRole('button', { name: 'Sign in with your browser' }).click()
  await expect(login.getByText('WDJB-MJHT', { exact: true })).toBeVisible()
  await page.evaluate(() => window.runtime.EventsEmit('desktop/browser-sign-in', { outcome: 'denied' }))
  await expect(login.getByRole('alert')).toHaveText('Sign-in was denied in the browser.')
  await expect(login.getByText('WDJB-MJHT', { exact: true })).toHaveCount(0)

  await login.getByRole('button', { name: '简体中文' }).click()
  await expect(login.getByRole('alert')).toHaveText('已在浏览器中拒绝此次登录。')
  await login.getByRole('button', { name: '在浏览器中登录' }).click()
  await expect(login.getByText('请在已打开的浏览器窗口中确认此次登录，并核对页面显示的代码与此一致。')).toBeVisible()
  await page.evaluate(() => window.runtime.EventsEmit('desktop/browser-sign-in', { outcome: 'expired' }))
  await expect(login.getByRole('alert')).toHaveText('登录请求已过期，请重新开始。')

  await login.getByRole('button', { name: 'English' }).click()
  await expect(login.getByRole('alert')).toHaveText('The sign-in request expired. Start again.')
  await login.locator('.login-page__local').click()
  await expect(login).toBeHidden()
})
