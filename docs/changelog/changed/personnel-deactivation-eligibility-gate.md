- Unattended Agent work now stops when its initiator loses authority — the
  account is disabled or removed from the run's Space. A run that has not started
  no longer starts a worker and instead reaches `CANCELED` (not `FAILED`) with a
  `cancel_reason` of `creator_disabled` or `creator_not_member`; a run already
  under way is asked to stop by a background reconciler and by the worker's own
  re-check when it fetches its run. A schedule whose creator lost authority
  pauses with a matching `pause_reason`. A run's managed-inference token is now
  attributed to the run's own initiator rather than the original Task creator, so
  a Continue by a colleague runs under that colleague.
