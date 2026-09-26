- A worker run whose trace and session could not be written to object storage
  now ends FAILED with the refused write named, keeping its reply, instead of
  reporting success with a trace link that could not load and a conversation
  the next Continue would silently lose.
