- With two Server replicas, a Conversation message sent while Redis is
  unreachable is now refused (HTTP 503, a WebSocket conversation error, or a
  retry hint in a chat channel) instead of answering 200 with an empty reply
  while the message was neither answered nor kept.
