- `buildmax --session-id <uuid>` now creates the session when it does not exist,
  matching the flag's documented "load if exists, else create" contract, so a
  caller can start a run under a deterministic id. `-r/--resume` still errors on
  an unknown id.
