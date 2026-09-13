- The OpenAPI specification is split along the listener boundary: the public
  `/openapi.json` no longer documents the worker control plane, which now lives
  in its own `openapi-worker.json`. This matches the two-listener network
  boundary, where the public socket cannot dispatch a worker route.
