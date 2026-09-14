- The Compose bundle now serves the Portal and the API through one gateway on a
  single origin (as the Kubernetes deployment already does), so the session
  cookie's same-origin check is satisfied and sign-in works out of the box; open
  the same published port as before.
