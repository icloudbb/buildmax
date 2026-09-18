- Corporate sign-in over OpenID Connect is now usable end to end (Okta the first
  supported provider): a "Sign in with <provider>" button on the Portal takes the
  browser through `GET /api/auth/oidc/start` and `…/callback`, which verifies the
  ID token, links or provisions the account, and opens the same session a
  password login would. Native password and login-code sign-in are gated
  independently by `local_login` (`all`, `system_admins`, `off`), so a deployment
  can run SSO only, both, or keep a break-glass path for operators. See
  [deploy/authentication.md](https://github.com/icloudbb/buildmax/blob/main/docs/deploy/authentication.md).
  The pinned real-Okta qualification and secret/key-rotation drills are still to
  come.
