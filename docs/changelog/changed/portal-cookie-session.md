- The Portal now keeps its refresh credential in a Secure, HttpOnly,
  SameSite=Strict cookie the browser manages instead of in `localStorage`, and
  holds only a short-lived access token in memory. New `/api/auth/portal/{login,
  session,logout}` routes deliver it; native CLI and Desktop clients keep using
  the JSON `/api/auth/*` routes. Serve the Portal and API from one origin (a
  reverse proxy in production; the dev server proxies `/api` automatically).
