---
id: <slug>
title: <one imperative line>
roadmap: <R0|R1|R2|R3|R4|R5|R6|none>
source: <docs/design/some-record.md#section, or "direct">
depends_on: []          # other task files, e.g. [20-other-task.md]
verification: []        # test scopes from docs/contribute/testing.md
claim:                  # "<handle> <YYYY-MM-DD>" while working; clear if abandoned
pr:                     # PR number once one is open; clear until then
---

## Outcome

What changes for the user or operator, and the evidence it matters. One or two
sentences.

## Scope

What this task does. Bounded enough that one session finishes it.

## Out Of Scope

What a reader might assume is included but is not, and where that work lives
instead.

## Acceptance Criteria

- Concrete, checkable statements of done.
- Each one is something a reviewer can confirm.

## Verification

The exact scopes to run and what they prove, chosen from
docs/contribute/testing.md. Name the narrowest check first, then any broader
scope the change requires.

## Notes

Links, prior context, or decisions the executing session should not have to
rediscover.
