---
id: portal-native-sign-in-confirmation
title: Confirm or deny a CLI or Desktop sign-in from Portal
roadmap: R5
source: docs/design/enterprise-identity-and-access.md#14-cli-and-desktop-sign-in
depends_on: [60-native-sign-in-server.md]
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

A person who opens the link or types the code from their CLI or Desktop sees
which client is asking, signs in to Portal if needed (including through SSO),
and explicitly confirms or denies it. This is the step where a human decides,
and it carries the flow's anti-phishing defenses (design §14.4).

## Scope

- A Portal hash route, `#/sign-in` (a code entry field) and `#/sign-in/<code>`
  (prefilled). It renders after the token gate but before the Space gate in
  `portal/src/App.tsx`, so an account with no usable Space can still confirm.
- The OIDC return path that design §8 describes and the shipped callback
  lacks:
  - `GET /api/auth/oidc/start` accepts a relative Portal hash route;
  - start validates it and keeps it inside the signed transaction cookie;
  - the callback redirects to it instead of `/`.
  - A local sign-in already keeps the hash.
- The request card. It shows:
  - the client kind (BuildMax CLI or Desktop);
  - the claimed client label, marked "reported by the client, not verified";
  - the code, large, with the compare-the-code instruction;
  - when the request started and when it expires;
  - the account it will sign in as, and when the resulting session ends;
  - the warning against links and codes someone else sent.
- **Confirm sign-in** and **Deny** call the confirm and deny routes. Nothing is
  decided on load.
- One "not valid any more" state covers wrong, expired, and decided codes. The
  throttled `429` gets its own message.
- After a decision, the page says to return to the client and clears the code
  from the URL, as `ChatAccountsSection` does.
- When less than one hour of the Portal session remains, the page says so and
  offers **Sign in again**, which signs Portal out and returns to the page.
- Strings in `portal/src/i18n` in English and Simplified Chinese, with the terms
  of design §14.1. The page says "confirm", not "approve", because "approval"
  already names Remote Control tool approvals.
- Update `manual/` (and `manual/zh/`) with the Portal half of the sign-in
  journey.

## Out Of Scope

- The server routes: [60](60-native-sign-in-server.md).
- A list of the person's own sessions: [68](68-self-service-sessions.md).
- Showing the requester's network address (design §19 item 9).

## Acceptance Criteria

- Opening `#/sign-in/<code>` signed out, signing in with the mock OIDC
  provider, and landing back on the same request works in kind. The local
  password path works the same way.
- The page never confirms or denies without a click.
- The claimed-label marker and the warning are visible in both locales.
- A confirmed or denied request shows the return-to-client state, and
  reloading the page does not show the code again.
- A non-Portal session cannot reach a confirm call from this page; the server
  refusal is covered in task 60.

## Verification

- `./make test` for the OIDC return-path validation (relative hash only, no
  external URL) in `internal/server/handlers/auth`.
- `./make check portal` for component tests of each page state.
- `./make e2e visual` for the new page in both locales, updating baselines
  intentionally.
- `./make e2e kind` on an ephemeral cluster with a browser case: start a
  request through the API, open the complete URI, sign in through the mock
  OIDC provider, confirm, and redeem with a poll.

## Notes

The Portal router is the custom hash router in `portal/src/router.ts`. The SSO
button in `portal/src/pages/auth/Login.tsx` navigates the full page to
`/api/auth/oidc/start`, which is where the return route must be passed.
