- Disabling an account whose cleanup partly fails now succeeds as what it is:
  the account is disabled, `user.disabled` is audited, the response names the
  failed steps in `cleanup_failed`, and Portal and `buildmax admin user disable`
  offer a safe retry instead of reporting an error for a change that took effect.
