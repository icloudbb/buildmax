---
id: drive-portal-authenticated-api-probe
title: Let the drive-portal driver probe an API endpoint with the page's auth
roadmap: none
source: direct
depends_on: []
verification: []
claim:
---

## Outcome

An agent driving Portal can read an endpoint's real authenticated status from
the browser session, instead of falling back to server logs to tell an
authorization failure from an unauthenticated probe. This makes exploratory
Portal findings self-contained — the difference that turned a 403 into a 401
during the null-space investigation cost a detour through `kubectl logs`.

## Scope

Add a driver command to `.buildmax/skills/drive-portal/driver.mjs` that issues a
`fetch` reusing the page's own Authorization header (the bearer token Portal
holds in memory), and prints the status and a short body snippet. Document it in
the skill's command table (`.buildmax/skills/drive-portal/SKILL.md`). A plain
`eval fetch(url, {credentials:"include"})` does not carry that token, so it
returns 401 on a page where the real request would succeed or 403.

## Out Of Scope

Any change to Portal itself or to the pass/fail Playwright suites; this is a
manual-exploration convenience for the ad hoc driver only.

## Acceptance Criteria

- The driver can report the authenticated status of an API path (e.g.
  `/api/spaces/<id>/members`) matching what the app's own requests receive.
- The skill's command table documents it and notes the plain-fetch pitfall.

## Verification

No automated suite covers the ad hoc drivers. Verify by hand: sign in with the
driver against a running deployment, probe a known-authorized and a
known-forbidden path, and confirm the reported statuses match the server's
request log for the same paths.

## Notes

Observed 2026-09-13 while investigating the null-space 403 (see
92-portal-null-space-conversation-guard.md): distinguishing the app's real 403
from the driver's unauthenticated 401 required reading the server request log,
because the driver had no way to replay a request with the page's credentials.
Lowest-priority of this batch; a convenience, not a correctness gap.
