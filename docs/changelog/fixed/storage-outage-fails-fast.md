- When object storage stops answering, an Artifact download and other storage
  calls now fail within seconds with HTTP 503 "object storage is unavailable"
  instead of hanging for minutes and then answering 500: the storage client
  bounds its connect, TLS, and response-header waits.
