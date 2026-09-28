- A run whose worker refuses it before starting, because a plugin could not be
  provided or a cancel arrived first, now ends at once; before, it stayed
  Scheduled until the six-hour run timeout. Every failed run now also records
  why it failed, and server logs for dispatch and reaping name its Space.
