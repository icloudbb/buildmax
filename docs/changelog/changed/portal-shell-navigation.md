- Portal's sidebar entries are links that open in a new tab and mark the
  current page; each page states its title once, with the breadcrumb leading
  back to its parents; page loads no longer log an expected 401 or 403
  (`POST /api/auth/portal/session` answers 204 without a cookie and returns
  the account's `system_roles`); and Portal and Desktop start in the operating
  system's light or dark theme until one is chosen.
