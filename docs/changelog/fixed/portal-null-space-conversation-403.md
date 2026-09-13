- The Portal dashboard no longer issues `GET /api/spaces/null/conversations` on
  first load, removing the 403 console error emitted on every sign-in before the
  current space resolves.
