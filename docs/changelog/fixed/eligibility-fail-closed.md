- Scheduled firings, run dispatch, and worker run fetches no longer start work
  when the initiator's eligibility cannot be checked because the account or
  membership store is unavailable; the work waits and is retried instead of
  running unverified.
