- When object storage becomes unreachable while the server holds idle
  connections to it, requests that read or write storage now fail with 503 in
  about half a minute instead of over a minute. Linux servers close a storage
  connection whose sent data goes unacknowledged for 10 seconds.
