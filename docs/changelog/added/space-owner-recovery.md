- A System Administrator can recover a shared Space whose recorded owners are
  all disabled by promoting an enabled member to owner, with
  `PUT /api/admin/spaces/{space_id}/owner` or, for break glass when the Server
  or IdP is unavailable, `buildmax-server space recover-owner <space_id>
  <successor_email>`. It refuses a personal Space, a Space whose owner can still
  sign in, and a successor who is not already an enabled member; it creates no
  membership and grants the operator no access to the Space's contents; and it
  records a `space.ownership_recovered` audit event naming the disabled owner,
  the successor, and the actor.
