- A Space Secret value a worker run quotes is now redacted from the run's
  reported error message, from the session metadata, session index, and run
  logs uploaded with its state, and from the trace's error and denial reasons;
  before, a failure quoting the value, a session title derived from the
  prompt, or a log line carried it in plain text.
