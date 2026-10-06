---
id: portal-administration-labels
title: Show Administration in human labels, with a next step for each failure class
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: [16-gui-shared-primitives.md]
verification: ["./make check portal", "./make e2e visual", "./make e2e local"]
claim:
pr:
---

## Outcome

An operator reading Administration sees labels in their language and knows
what to tell whoever must act on a failure. Administration stays
metadata-only. This task carries Portal audit findings P5 and P24.

## Findings

**P5 (Minor). Administration shows raw identifiers and gives no next step for
a failure.**

- Screen: Administration → Overview and Spaces, as `alice@buildmax.local`
  (System Administrator) with `./make kind fixtures --runs` data.
- Raw values on the page:
  - `DATABASE`, `OBJECT_STORAGE`, and `ok` as health labels;
  - `CANCELED`, `FAILED`, `RUNNING`, and `SUCCEEDED` as Task-run counts;
  - role `system_admin`, worker mode `k8s_job`, transport `direct`, and tiers
    `pro` and `free_trial`;
  - run IDs in the "Spaces needing attention" table.
- These stay untranslated in Chinese. The quota "258 / 10000000" is not
  formatted, and "1 running" wraps as "1 runnin g".
- The FAILED cell ("3 space configuration") names who acts, but not what to
  tell them. Metadata-only is the design, so the gap is the wording, not
  access.

**P24 (Cosmetic). Administration Space links use the browser's default blue
underline**, unlike every other link in Portal.

## Scope

- Map health, run status, role, worker mode, transport, and tier values to
  catalog labels in both locales. Run statuses use task 16's `StatusLabel`
  over the shared vocabulary. Raw values may stay as secondary technical
  detail.
- Format quota numbers for the locale, and stop the count cells from wrapping
  mid-word.
- For each failure class, add a short next step that names who acts and what
  they check. For example, a Space configuration failure tells the Space
  owner to check the Agent's Secret grants. Do not add a route to run content.
- Use a human label instead of raw run IDs in "Spaces needing attention", or
  move the IDs to secondary, copyable metadata.
- Give Space links the shared link style.

## Out Of Scope

- Any new access from Administration to Space content.
  [System administration](../design/system-administration.md) keeps it
  metadata-only.
- The Space-side explanation of the same failure, which is
  [task 32](32-portal-failure-explanation.md).

## Acceptance Criteria

- No raw enum value is the primary label anywhere on Overview, Spaces, or
  Space detail, in English or zh-CN.
- Every failure class shown has a next-step line.
- Quota values are formatted, and no count cell breaks mid-word at 1280 px.
- Admin Space links match the shared link style in both themes.

## Verification

1. `./make check portal`.
2. `./make e2e visual`. The diagnostics template baseline may change on
   purpose.
3. `./make e2e local`.

## Notes

This task depends on task 16 for `StatusLabel` and the single status
vocabulary.

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
