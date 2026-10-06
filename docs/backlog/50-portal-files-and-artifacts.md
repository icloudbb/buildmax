---
id: portal-files-and-artifacts
title: Let a person name, link, and return to a Files folder, and find shared Artifacts
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

Files has one translated root name, and a folder has its own URL that survives
a reload. The Artifacts list shows which Artifacts are shared and can be
filtered to them. This task carries Portal audit findings P7 and P19.

## Findings

**P7 (Minor). The Files root is called "home", and the folder is not in the
URL.**

- Screen: Files.
- Actual:
  - In English, both the tree root and the panel title read "home".
  - In Chinese, the panel title reads "根目录" while the tree root still reads
    "home".
  - Opening a folder does not change `#/…/files`, so a reload returns to the
    root.

**P19 (Minor). Shared Artifacts cannot be found.**

- Screen: Artifacts.
- Reproduction: open Artifacts in a Space where some Artifacts have public
  links.
- Actual: the list shows no share state and has no search or filter. Finding
  a shared Artifact means opening each Artifact's **Share** dialog in turn.

## Verified Facts

These were checked on `main` at `e36fa722`. The Artifact list response
(`internal/server/handlers/artifact/routes.go`, `artifactResponse`) carries a
`share` object only on an upload that asked for a link. Listed items carry no
share state, and `GET /api/spaces/{space_id}/artifacts` accepts no filter. P19
therefore needs a server change.

## Scope

- Give the Files root one person-facing name from the catalog in both places.
  Do not use "home" unless that is the chosen word.
- Put the open folder's path in the Files route, so reload, back and forward,
  and a copied link land on the same folder. This follows the navigation
  record's rule that a reload preserves the same Space and resource.
- Add share state to listed Artifacts, such as whether an active public link
  exists and when it expires, without returning the token. Add a "shared"
  filter to the list route. Update `openapi.json` and the handler tests.
- Show the share state on each row, and add the filter, plus a name search if
  it fits the same query, to the Artifacts page.

## Out Of Scope

- Changing how sharing works or who may share.
- Upload, which needs a native file chooser that the audit could not drive.

## Acceptance Criteria

- The tree root and the panel title agree in both locales.
- Opening a folder and reloading keeps that folder open. Back returns to the
  parent.
- The Artifacts list marks shared Artifacts and can show only them. No share
  token appears in list responses.

## Verification

1. `./make test` for the Artifact list change.
2. `./make check portal`, then `./make e2e visual`.
3. Use the kind loop, then `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
