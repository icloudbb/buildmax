- `./make ocean deploy` brings up a server that starts and workers that run:
  it now sets the required worker disk bounds, serves the worker API on its
  TLS listener behind a worker-only NetworkPolicy, installs the worker seccomp
  profile on every node, and defaults to the `v0.2.0-alpha.17` images.
