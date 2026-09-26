- Rotating the JWT secret no longer signs CLI, Desktop, and Remote Control users
  out: a signed-in client renews once when the server refuses its access token
  and retries, the Portal WebSocket renews before reconnecting, and
  `access_token_ttl` now defaults to the documented 15 minutes instead of a
  week. Runs in flight during a rotation are still lost unless drained.
