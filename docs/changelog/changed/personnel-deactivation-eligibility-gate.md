- Unattended Agent work now stops when its initiator loses authority: a run
  whose account was disabled or removed from the run's Space no longer starts a
  worker and instead reaches `CANCELED` — not `FAILED` — with a `cancel_reason`
  of `creator_disabled` or `creator_not_member`. A schedule whose creator was
  disabled or removed from its Space pauses with a matching `pause_reason`. A
  run's managed-inference token is now attributed to the run's own initiator
  rather than the original Task creator, so a Continue by a colleague runs under
  that colleague.
