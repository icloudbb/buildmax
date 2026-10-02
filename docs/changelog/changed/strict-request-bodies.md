- The HTTP API now refuses a JSON request body that carries a field the
  endpoint does not take, with a 400 naming it (`unknown field "x"`), instead
  of ignoring it: a misspelled field such as a Secret grant under the wrong name
  no longer creates something without the setting it was meant to have.
  Logout also refuses a misspelled `refresh_token` instead of leaving that
  session alive.
