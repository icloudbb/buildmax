- The server API for signing the CLI and Desktop in through the Portal:
  `/api/auth/native-sign-ins` starts a request, a signed-in Portal session
  confirms or denies it, and the client's poll redeems it for a session that
  inherits the confirming session's sign-in method and expiry. It is
  groundwork: the Portal page and the CLI and Desktop sign-in that use it come
  in later releases. `POST /api/auth/login` accepts an optional
  `client_label`, which the admin session list shows.
