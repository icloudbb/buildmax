- Kubernetes worker Job pods now set `enableServiceLinks: false`, so the
  addresses of the namespace's other Services — database, object storage, cache
  — are no longer written into the environment that model-chosen commands can
  read.
