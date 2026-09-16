- Unattended Agent work now stops when its initiator loses authority: a run
  whose account was disabled or removed from the run's Space no longer starts a
  worker, and a schedule whose creator was removed from its Space pauses like
  one whose creator was disabled. A run's managed-inference token is now
  attributed to the run's own initiator rather than the original Task creator, so
  a Continue by a colleague runs under that colleague.
