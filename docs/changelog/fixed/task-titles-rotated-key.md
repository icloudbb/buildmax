- Task titles now use a catalog model key replaced with `model set-key` from the
  next task, like every other model call. They used the key the server started
  with until it restarted, so revoking the old key after a rotation silently
  degraded every new task's title.
