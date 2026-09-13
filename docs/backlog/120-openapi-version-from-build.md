---
id: openapi-version-from-build
title: Stamp OpenAPI info.version from the application version source
roadmap: none
source: docs/design/api-surface-conventions.md#4-decision-no-url-versioning-yet
depends_on: []
verification: ["./make test ./internal/server/handlers", "./make build"]
claim:
pr:
---

## Outcome

The OpenAPI `info.version` reflects the running build instead of a hand-set
literal that drifts. Today it is `0.0.7`, tied to nothing and read by nothing.

## Scope

Bind `info.version` to the single application-version source — the git tag
`tools/mk` already injects into the `config.Version` build variable at link time
(design §4).

- Either the build rewrites the served spec or the `GET /openapi.json` handler
  injects `config.Version` at serve time; choose the simpler wiring.
- Remove the hand-maintained `0.0.7` literal so there is no second source.
- OpenAPI 3.0 requires `info.version`, so it stays present, sourced from
  `config.Version`.

## Out Of Scope

URL versioning of any kind (design §4 rejects it). The OpenAPI split (task 110);
this task must work whether the spec is one document or two.

## Acceptance Criteria

- The served OpenAPI `info.version` equals `config.Version` for the running
  binary.
- No hand-set version literal remains in the checked-in spec.

## Verification

`./make test ./internal/server/handlers` covers the serve-time value; `./make
build` confirms the version-injection build path still links.

## Notes

`config.Version` is set at link time by `tools/mk`; a dev build's value is the
non-release default, which is expected and fine for this check.
