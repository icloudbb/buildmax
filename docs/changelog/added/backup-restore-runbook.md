- A backup-and-restore runbook for server deployments: what to back up (the
  database schema, the bucket prefix, the KEK, and `server.yaml`), the database
  snapshot before the bucket copy with a no-delete window, a separate recovery
  bucket, no Telegram token in recovery, and verification with
  `buildmax-server storage verify --checksums`. `./make kind drill restore`
  rehearses it end to end on an ephemeral kind cluster and reports the recovery
  time and any loss.
