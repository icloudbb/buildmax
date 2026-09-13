---
id: webhook-keys-handler-relocation
title: Relocate the webhook-keys handler to an account-level package
roadmap: none
source: docs/design/api-surface-conventions.md#65-non-route-follow-ups
depends_on: []
verification: ["./make test ./internal/server/handlers", "./make test ./internal/architecture"]
claim:
pr: 611
---

## Outcome

The `webhook-keys` handler lives in a package that matches its ownership. The
routes are account-scoped (`user_webhook_key` keys every row by `user_id`, design
§3.3), but the handler currently sits in the `space` package, which reads as if
webhook keys were space-owned.

## Scope

Move the `/api/webhook-keys` handlers out of `internal/server/handlers/space`
into a fitting account-level package, holding only the store those routes need.

- The routes do not change (`GET`/`POST /api/webhook-keys`,
  `DELETE /api/webhook-keys/{key_id}`); clients are unaffected.
- Keep the package's Config narrow: it should not gain reach into space data it
  does not use, consistent with the ownership boundaries in AGENTS.md.
- Update `RegisterPublic` composition if the registration site moves.

## Out Of Scope

Any route, request/response, or authorization change. The top-level placement is
correct and stays (design §3.3, §6.4).

## Acceptance Criteria

- The webhook-keys handler no longer lives in the `space` package.
- The routes and their behavior are unchanged; the architecture route/spec match
  still passes.
- The new package's Config does not expose space stores the handler does not use.

## Verification

`./make test ./internal/server/handlers` for handler behavior; `./make test
./internal/architecture` for the route/spec match and package-boundary checks.

## Notes

Internal move only. This is the one non-route follow-up in the reconciliation; it
can land independently of the route renames.
