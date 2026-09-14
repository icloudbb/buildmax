- Fixed cross-origin sign-in: the server's CORS responses now set
  `Access-Control-Allow-Credentials: true`, so a deployment that serves the
  Portal and the API on different origins (such as the Compose bundle) can send
  the session cookie and complete login, which the browser had been blocking.
