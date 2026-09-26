- `buildmax-server secret rewrap` moves every stored Space Secret and model
  credential onto the KEK file's current key, and the server now refuses to
  start while a stored row names a key the file does not hold.
