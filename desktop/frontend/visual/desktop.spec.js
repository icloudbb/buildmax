import { test, expect } from '@playwright/test'

import { expectAccessible, ORIGIN, serve, settle } from './harness.js'

// What the Desktop app looks like along its golden path: the home a fresh
// install opens on, the same home with work in it, the New Project dialog, and
// the server sign-in page in each of its layouts and both languages, in both
// themes. A difference from the committed
// baseline fails until it is accepted with `./make e2e visual --update` and
// reviewed with the change that caused it.

// A fresh BUILDMAX_HOME: nobody signed in, nothing opened yet. This is the
// state the desktop-ui suite's golden path also starts from.
const fresh = {
  GetAuthStatus: { logged_in: false },
  ListProjects: [],
  ListSessions: [],
  ListLaunchpadEntries: [],
}

const worked = {
  ...fresh,
  ListProjects: [
    { id: 'prj_engine', name: 'analytical-engine', kind: 'git', locator: '/home/ada/src/analytical-engine', default_workspace: '/home/ada/src/analytical-engine', last_used_at: '2026-03-04T14:10:00Z' },
    { id: 'prj_notes', name: 'notes', kind: 'directory', locator: '/home/ada/notes', default_workspace: '/home/ada/notes', last_used_at: '2026-03-01T09:00:00Z' },
  ],
  ListSessions: [
    { id: 'ses_1', project_id: 'prj_engine', kind: 'user', title: 'Tighten the retry policy', workspace: '/home/ada/src/analytical-engine', created_at: '2026-03-04T13:00:00Z', updated_at: '2026-03-04T14:10:00Z' },
    { id: 'ses_2', project_id: 'prj_engine', kind: 'user', title: 'Explain the scheduler lease', workspace: '/home/ada/src/analytical-engine', created_at: '2026-03-03T10:00:00Z', updated_at: '2026-03-03T11:00:00Z' },
    { id: 'ses_3', project_id: 'prj_notes', kind: 'user', title: 'Outline the 0.3 release note', workspace: '/home/ada/notes', created_at: '2026-03-01T09:00:00Z', updated_at: '2026-03-01T09:30:00Z' },
  ],
}

const views = [
  {
    name: 'home-fresh',
    bridge: fresh,
    open: async (page) => {
      await expect(page.getByText('No recent chats yet.')).toBeVisible()
    },
  },
  {
    name: 'home',
    bridge: worked,
    open: async (page) => {
      await expect(page.getByText('Tighten the retry policy').first()).toBeVisible()
    },
  },
  {
    name: 'new-project',
    bridge: fresh,
    open: async (page) => {
      await page.locator('.page-home__primary').click()
      await expect(page.locator('.modal-panel').getByRole('heading', { name: 'New Project' })).toBeVisible()
    },
  },
  {
    name: 'sign-in',
    // No server configured on this machine, so the address field is empty and
    // the page leads with the password form.
    bridge: { ...fresh, GetDefaultServerURL: '' },
    open: openSignIn,
  },
  {
    // A deployment where SSO is the only way in: the browser button alone.
    name: 'sign-in-sso',
    bridge: signedOutAt({ local_login: 'off', oidc: { enabled: true, display_name: 'Okta' } }),
    open: async (page) => {
      await openSignIn(page)
      await expect(page.getByRole('button', { name: 'Continue with Okta in your browser' })).toBeVisible()
    },
  },
  {
    // Native passwords for administrators only, in Chinese: the browser leads.
    name: 'sign-in-admins-zh',
    bridge: signedOutAt({ local_login: 'system_admins', oidc: { enabled: true, display_name: 'Okta' } }),
    open: async (page) => {
      await openSignIn(page)
      await page.getByRole('button', { name: '简体中文' }).click()
      await expect(page.getByRole('button', { name: '管理员使用密码登录' })).toBeVisible()
    },
  },
  {
    name: 'sign-in-waiting',
    bridge: signedOutAt({ local_login: 'off', oidc: { enabled: false } }),
    open: async (page) => {
      await openSignIn(page)
      await page.getByRole('button', { name: 'Sign in with your browser' }).click()
      await expect(page.getByText('WDJB-MJHT', { exact: true })).toBeVisible()
    },
  },
  {
    name: 'sign-in-waiting-zh',
    bridge: signedOutAt({ local_login: 'off', oidc: { enabled: false } }),
    open: async (page) => {
      await openSignIn(page)
      await page.getByRole('button', { name: '简体中文' }).click()
      await page.getByRole('button', { name: '在浏览器中登录' }).click()
      await expect(page.getByRole('button', { name: '重新打开浏览器' })).toBeVisible()
    },
  },
]

async function openSignIn(page) {
  await page.locator('.sidebar__user-trigger').click()
  await page.getByText('Sign in to a server', { exact: true }).click()
  await expect(page.locator('.login-page')).toBeVisible()
}

// A machine configured with a server whose sign-in methods are `methods`, and
// a browser sign-in that starts with code WDJB-MJHT.
function signedOutAt(methods) {
  return {
    ...fresh,
    GetDefaultServerURL: 'https://buildmax.example.com',
    GetAuthMethods: methods,
    StartBrowserSignIn: {
      user_code: 'WDJB-MJHT',
      verification_uri: 'https://buildmax.example.com/#/sign-in',
      verification_uri_complete: 'https://buildmax.example.com/#/sign-in/WDJB-MJHT',
      expires_in: 600,
    },
  }
}

for (const theme of ['light', 'dark']) {
  test.describe(theme, () => {
    test.use({ viewport: { width: 1280, height: 800 } })

    for (const view of views) {
      test(view.name, async ({ page }) => {
        const unanswered = await serve(page, theme, view.bridge)
        await page.goto(`${ORIGIN}/`)
        await settle(page)
        await view.open(page)
        await settle(page)
        await expect(page).toHaveScreenshot(`${view.name}-${theme}.png`)
        // Contrast and the rest of WCAG A/AA on the same rendered view.
        await expectAccessible(page)
        expect(unanswered).toEqual([])
      })
    }
  })
}
