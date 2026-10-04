- Kubernetes worker Jobs no longer carry the object-storage keys or the direct
  provider key as plain values: each is a reference to the Secret named by
  `worker.k8s.credential_secret` (default `buildmax-secret`), so reading a Job
  or Pod no longer reveals them
  ([configuration](https://github.com/icloudbb/buildmax/blob/main/docs/reference/configuration.md)).
