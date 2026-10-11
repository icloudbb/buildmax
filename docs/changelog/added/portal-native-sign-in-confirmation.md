- A Portal page, `#/sign-in/<code>`, where a person confirms or denies a CLI
  or Desktop sign-in after checking the client, its unverified label, and the
  code, in English and Simplified Chinese; opening it signed out returns to
  the request after any sign-in, as SSO now honors a validated relative hash
  route passed to `GET /api/auth/oidc/start` as `return_to`.
