---
id: assistant-readable-files
title: Let an Assistant read its allowlisted Space files at the front door
roadmap: R5
source: docs/design/space-assistants.md#10-the-front-door-turn
depends_on: [66-assistant-front-door-turn.md]
verification: ["./make test", "./make check docs"]
claim: gougoujiang 2026-10-04
pr:
---

## Outcome

An Assistant answers policy questions ("what is the leave policy") from material
the Space chose, in the Server, without starting a worker.

## Scope

- ListFiles and ReadFile tools in Assistant turns, limited to the Assistant's
  readable-files allowlist; text media types only; bounded bytes per read and
  per turn; output written for the LLM on success and refusal.
- Tool names added to `internal/tool/names.go` if they are runtime names, and to
  the tool inventory documentation.

## Out Of Scope

- Search, chunking, embeddings, or any knowledge base.

## Acceptance Criteria

- An Artifact outside the allowlist, in or outside the Space, is unreadable by
  id, name, or listing.
- Binary and oversized files are refused with a clear tool result.
- Manual and design (with zh) updated.

## Verification

`./make test ./internal/service/conversation/...`, then `./make test`;
`./make check docs`.
