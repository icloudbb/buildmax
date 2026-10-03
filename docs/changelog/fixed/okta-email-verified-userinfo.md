- Okta single sign-on no longer refuses every first sign-in: when the ID Token
  carries an email without `email_verified`, as Okta's org authorization server
  sends it, BuildMax now reads the flag from UserInfo. A refused SSO sign-in's
  specific reason is now in the server log.
