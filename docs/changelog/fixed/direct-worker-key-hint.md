- A task run on a `direct`-transport worker whose model key is refused or
  missing now says to check `conversation.model.api_key` in `server.yaml` or
  `BUILDMAX_CONVERSATION_MODEL_API_KEY`, where that key actually lives; before,
  it pointed at `api_key` in a `settings.yaml` the worker never reads.
