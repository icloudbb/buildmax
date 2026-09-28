- When the server ends a run whose worker went silent, never confirmed a cancel,
  or never reported within `worker.run_timeout`, it now deletes the run's
  Kubernetes Job, so a hung or partitioned worker no longer keeps its pod
  running.
