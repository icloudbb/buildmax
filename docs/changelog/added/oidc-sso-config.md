- Groundwork for corporate sign-in over OpenID Connect: a server `oidc` block
  (issuer, client, `provisioning`, `allowed_email_domains`, `session_max_age`),
  a `local_login` knob (`all`, `system_admins`, `off`) that gates native
  password and login-code sign-in independently of SSO, and a new unauthenticated
  `GET /api/auth/methods` that reports the enabled sign-in methods. The client
  secret is injected with `BUILDMAX_OIDC_CLIENT_SECRET` and never served; the
  admin system view reports the provider's live health. The browser login flow
  itself lands in a following change.
