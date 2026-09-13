- `buildmax -r <value>` with a malformed (non-UUID) session id now reports an
  "invalid resume id" usage error instead of the "session not found" a
  well-formed but unknown id gets, matching the `--session-id` validation.
