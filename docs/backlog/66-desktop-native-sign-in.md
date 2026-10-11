---
id: desktop-native-sign-in
title: Offer browser sign-in on the Desktop sign-in page
roadmap: R5
source: docs/design/enterprise-identity-and-access.md#147-client-experience
depends_on: [60-native-sign-in-server.md]
verification: ["./make test", "./make e2e desktop", "./make check desktop", "./make e2e desktop-ui", "./make e2e visual", "./make build desktop"]
claim:
pr:
---

## Outcome

Desktop can sign in to any server a person can use in Portal. The sign-in page
reads the server's methods and offers **Sign in with your browser** whenever
native password login is restricted or off, so an SSO user is never shown only
a password form that will refuse them.

## Scope

- Go bindings in `internal/interface/desktop`:
  - one that reads `GET /api/auth/methods` for a server URL;
  - one that starts a native sign-in request and opens the system browser at
    its complete URI through the Wails `BrowserOpenURL` runtime call;
  - a cancel binding;
  - Go-side polling that saves the login through the existing `saveLogin` path
    and emits completion, denial, or expiry events.
- `desktop/frontend/src/LoginPage.jsx`, laid out by `local_login` (design
  §14.7):
  - `all`: the password form first, as today, with the browser button below
    the divider;
  - `system_admins`: the browser button first, and the password form behind
    "Administrator sign-in with a password";
  - `off`: the browser button only;
  - with OIDC enabled, the button names the provider.
- The waiting panel: the code, the instruction, **Open browser again**, **Copy
  link**, and **Cancel**. The denied and expired states return to the form. A
  failed methods request shows the existing unreachable-server state.
- Send `client_label` (host name and OS) on browser sign-in and password login.
- Strings in `desktop/frontend/src/i18n/login.js` in English and Simplified
  Chinese.
- Update `docs/contribute/architecture/desktop.md`, which says Desktop never
  reads the methods, and the Desktop sign-in pages in `manual/` and
  `manual/zh/`.

## Out Of Scope

- Server routes: [60](60-native-sign-in-server.md). Portal page:
  [62](62-portal-native-sign-in-confirmation.md).
- Desktop Remote Control opt-in (see the Remote Control design).
- A deep-link or custom URL scheme back into Desktop. Polling completes the
  sign-in, so none is needed.

## Acceptance Criteria

- Against a server with `local_login: off`, the page shows only the browser
  option, and a confirmation in Portal signs Desktop in.
- Against `local_login: all`, the password form leads, as it does today.
- Cancel stops polling, and no session is created.
- Expired and denied requests show their messages in both locales.
- The packaged app builds and launches.

## Verification

- `./make test` for the new bindings' polling and event logic.
- `./make e2e desktop` for the bridge.
- `./make check desktop` and `./make e2e desktop-ui` for each `local_login`
  layout and the waiting panel, against a stubbed bridge.
- `./make e2e visual` for both locales.
- `./make build desktop`, then `./make e2e desktop-launch` if packaging
  changed.

## Notes

Desktop currently never calls `/api/auth/methods`, and nothing in it uses the
Wails `BrowserOpenURL` runtime call yet. The frontend wrapper
`desktop/frontend/src/lib/wailsRuntime.js` exposes only events, so the Go
binding should open the URL.
