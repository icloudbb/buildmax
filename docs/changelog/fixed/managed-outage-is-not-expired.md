- Managed mode: a deployment that is down or restarting is no longer reported as
  an ended login. The CLI says the login still works and offers retry or
  `buildmax logout`; Desktop keeps the workbench open under a banner that
  retries on its own, instead of a sign-in screen whose only way out discarded a
  working login. A login the server rejects stays in place until you sign in
  again or out, rather than being cleared so the next command silently ran in
  local mode, and a disabled account gets its own message.
