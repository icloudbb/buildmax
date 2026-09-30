- The worker image now ships `curl`, so an Agent can fetch over HTTPS through
  the sandbox proxy under the `registries`/`open` network tier; Alpine's busybox
  `wget` cannot tunnel HTTPS through a proxy and returned 502.
