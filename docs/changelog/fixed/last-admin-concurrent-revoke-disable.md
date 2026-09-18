- A system administrator's grant can no longer be revoked at the same moment
  another administrator's account is disabled in a way that left the deployment
  with no effective administrator; the last-holder guard now serializes the two
  correctly.
