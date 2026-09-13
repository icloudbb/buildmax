---
id: portal-null-space-conversation-guard
title: Stop Portal fetching conversations for a null space id on first load
roadmap: none
source: direct
depends_on: []
verification: ["./make check portal", "./make e2e kind"]
claim:
---

## Outcome

A freshly loaded Portal dashboard no longer issues
`GET /api/spaces/null/conversations`, so it stops emitting a 403 console error
and a failed request on every sign-in. This also restores `console errors` as a
trustworthy pass/fail signal for the drive-portal skill and Portal specs, which
today fires on a known-good page.

## Scope

Guard the conversations fetch on a resolved space id.
`portal/src/hooks/useConversations.ts` calls
`getConversations(currentSpaceId!, token!, …)` but enables on
`enabled && !!token`; when `token` resolves before `currentSpaceId`, it fires
with `currentSpaceId === null`, and `encodeURIComponent(null)` becomes the
literal string `"null"` in
`portal/src/features/conversations/api.ts`. Gate on
`enabled && !!token && !!currentSpaceId` and drop the `currentSpaceId!`
non-null assertion. Check for the same missing-guard pattern in sibling
`useAsyncList` hooks that take a nullable space id.

## Out Of Scope

Server-side handling of an unknown space id (returning 403 for a non-member is
acceptable); this task removes the bad client request, not the server's refusal.

## Acceptance Criteria

- Loading the dashboard immediately after sign-in produces no
  `/api/spaces/null/...` request and no 403 console error.
- The conversations list still loads once the current space resolves.

## Verification

`./make check portal` for the hook unit test (no fetch when spaceId is null);
`./make e2e kind` for a dashboard-loads-clean assertion through the deployed
ingress. Consider a Portal spec asserting `console errors` is empty on first
dashboard load.

## Notes

Confirmed 2026-09-13 on `main` cceba61c against an ephemeral kind cluster as
alice: console logged `Failed to load resource: 403 (Forbidden)`; the request
was `GET /api/spaces/null/conversations?limit=100`, and the server log recorded
`path=/api/spaces/null/conversations status=403`. The page recovers once the
space resolves, so end-user impact is low, but the console-error noise on every
load is what makes the drive-portal "check console errors before declaring
success" guidance unreliable.
