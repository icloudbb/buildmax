- `buildmax login` and the Desktop sign-in page sign in through a request you
  confirm in Portal, so single sign-on accounts can use the CLI and Desktop
  even where `local_login` is `system_admins` or `off`; the CLI opens the
  browser unless `--no-browser` is set or the host is headless,
  `buildmax login --password` keeps the password and login-code prompts, and
  Desktop lays its page out by the server's sign-in methods.
