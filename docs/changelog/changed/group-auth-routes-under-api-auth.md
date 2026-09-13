- Authentication routes moved under a common `/api/auth/` prefix:
  `/api/auth/otp`, `/api/auth/login`, `/api/auth/logout`, `/api/auth/password`,
  and `/api/auth/token/refresh`. The old top-level paths (`/api/login`,
  `/api/otp/request`, and the rest) no longer exist; clients that ship with the
  server were updated in step.
