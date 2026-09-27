- Kubernetes worker Jobs and their pods are now deleted after
  `worker.k8s.finished_job_ttl` (5 minutes by default) instead of accumulating
  in the worker namespace indefinitely.
