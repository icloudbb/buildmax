- A workflow run's per-step records are now node runs: the API and Portal
  expose `node_id`, `node_index`, and `node_type`, each run records the full
  resolved input its node received and its complete output (replacing the
  truncated output summary), and the run detail shows both.
