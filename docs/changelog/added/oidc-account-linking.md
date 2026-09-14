- SSO account linking: a new `external_identity` table binds a BuildMax account
  to a verified `(issuer, subject)` at the IdP, and an association step resolves
  a verified sign-in to its account — reusing an existing link, linking an
  operator-created account by verified email, refusing a takeover, or creating an
  account just in time within `allowed_email_domains`. A System Administrator can
  list an account's identity links and, while the account is disabled, unlink one
  (`GET`/`DELETE /api/admin/users/{user_id}/identities`); linking and unlinking
  are recorded in the same transaction as the change. The browser sign-in flow
  that drives this lands in a following change.
