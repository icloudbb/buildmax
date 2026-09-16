- A System Administrator can preview what disabling an account would stop with
  `GET /api/admin/users/{user_id}/deactivation-impact` — live sessions, webhook
  keys, memberships and roles, sole-owned Spaces, enabled schedules, active runs
  by status, and the cancellation bound, as counts and ids only, never Space
  content. Disabling an account (`PUT .../state`) now commits the account gate
  and then, in one orchestrated step, revokes sessions, pauses the account's
  schedules, cancels its in-flight runs, and — when `retire_webhook_keys` is set
  for a leaver rather than a suspension — permanently retires its webhook keys;
  the response reports the gate result alongside those cleanup counts.
