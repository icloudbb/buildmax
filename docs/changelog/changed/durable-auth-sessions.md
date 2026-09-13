- Server sessions are now durable and checked on every request: logout,
  administrator revocation, and account disablement stop an already-issued
  access token within the access-token window instead of at its expiry. Access
  tokens default to 15 minutes and sessions have a 90-day absolute lifetime
  (`access_token_ttl`, `session_absolute_ttl`). Existing sessions must sign in
  again after the upgrade.
