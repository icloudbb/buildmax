---
id: storage-reference-check
title: Add a read-only check that stored references resolve to their objects
roadmap: R2
source: docs/design/verification-program.md
depends_on: []
verification: ["./make test", "./make test mysql"]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

After a restore, an operator without source knowledge can prove V19 — every
retained reference resolves or is explicitly reported — with one command.
Maintainer decision 2026-09-26: read-only check, no repair.

## Scope

A `buildmax-server` subcommand that walks Artifact `storage_key`/`sha256`,
workspace checkpoint `storage_key`/`payload_sha256`, `task_run.trace_path`, and
plugin release `object_key`/digest; reports missing objects and checksum
mismatches with counts and IDs; exits non-zero on any finding. Bounded and
paged; checksum verification is optional (it reads every byte).

## Out Of Scope

Repair or tombstoning. The restore drill (task 22).

## Acceptance Criteria

- Tests cover resolved, missing, and mismatched references for each kind.
- Documented in the operator docs and `manual/` CLI reference as required by
  the architecture tests.

## Verification

`./make test`, `./make test mysql`, `./make lint`, `./make check docs`.
