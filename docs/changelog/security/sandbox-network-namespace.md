- On Linux, a sandboxed Bash command now runs in its own network namespace
  whose only way out is the sandbox proxy, unless the network policy allows
  every host: a command that ignores `HTTP_PROXY` can no longer reach the
  network, object storage, or the API directly. `bwrap` run as root now drops
  every capability before the command (it previously kept `SYS_ADMIN`), and
  the worker's own environment is no longer readable through
  `/proc/<pid>/environ`. Worker pods add `NET_ADMIN`; Compose deployments
  should add it to the server container as `compose.yaml` now does.
