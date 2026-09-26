- `buildmax-server` — the server and its `user` and `space` commands — now
  refuses to start against a database a newer release has migrated, naming the
  migrations it does not know, instead of letting its startup re-add what they
  dropped (rolling 0.2.0-alpha.15 back to 0.2.0-alpha.14 re-adds
  `schedule.agent_id NOT NULL` filled with 0: every schedule disappears, and
  creating one fails after rolling forward again). Binary rollback is not
  supported: back up the database and bucket together before upgrading, and
  recover by restoring both and running the matching binaries.
  `database.allow_newer_schema: true` in `server.yaml` overrides the refusal for
  a deliberate recovery. Releases up to 0.2.0-alpha.15 predate the refusal, and
  their destructive migrations cannot be undone by any binary: 0.2.0-alpha.13
  dropped `workflow_step_run` and the step runs recorded in it, and
  0.2.0-alpha.15 dropped `schedule.agent_id` and `last_task_id` after moving
  them into the executor columns.
