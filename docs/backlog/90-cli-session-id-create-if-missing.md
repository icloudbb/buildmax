---
id: cli-session-id-create-if-missing
title: Make CLI --session-id create a missing session as documented
roadmap: none
source: direct
depends_on: []
verification: ["./make test ./internal/interface/cli", "./make e2e cli"]
claim:
---

## Outcome

`buildmax --session-id <uuid>` follows its documented contract — "load if
exists, else create" — so a caller that picks a deterministic session id up
front (the reason this flag, unlike `-r/--resume`, requires a valid UUID) can
start that session instead of hitting an error. Today it fails, which blocks
scripted and reproducible-id workflows.

## Scope

Route a caller-supplied `--session-id` through the existing
`AgentApp.OpenOrCreateSession`, in both print mode
(`internal/interface/cli/print.go`) and the TUI
(`internal/interface/cli/tui.go`). `internal/interface/cli/root.go` currently
collapses `--session-id` and `-r/--resume` into one `effectiveSessionID` that
both surfaces open with the open-only `AgentApp.OpenSession`; carry the
distinction through so `--session-id` creates on miss while `-r/--resume` stays
open-only.

## Out Of Scope

Any change to `-r/--resume` semantics (it must keep erroring on an unknown id),
and any change to the worker path, which already calls `OpenOrCreateSession`
(`internal/agentapp/taskrun/runtime.go`).

## Acceptance Criteria

- `buildmax --session-id <fresh-uuid> -p "hi"` creates the session and runs,
  and a second invocation with the same id reuses it. Same in TUI mode.
- `buildmax -r <unknown-id> -p "hi"` still fails with the existing
  session-not-found error.
- An invalid (non-UUID) `--session-id` still fails with the existing
  "invalid session-id" usage error.

## Verification

`./make test ./internal/interface/cli` for the session-target/root wiring and a
new create-on-miss case; `./make e2e cli` for the print + TUI end-to-end paths.

## Notes

Confirmed 2026-09-13 on `main` cceba61c: `buildmax --session-id $(uuidgen)
-p hello` returns `error: session not found` (exit 4). Help text and flag
description (`internal/interface/cli/root.go:32` and `:86`) both promise
"load if exists, else create". The implementing method
`AgentApp.OpenOrCreateSession` already exists and is unit-tested
(`TestOpenOrCreateSessionUsesAssignedID`) but is only called by the worker.
Check `docs/contribute/architecture/cli.md` for any restatement of the
create-on-miss contract so code and doc stay aligned (there is no user CLI
manual today; the contract lives in the flag help in `root.go`).
