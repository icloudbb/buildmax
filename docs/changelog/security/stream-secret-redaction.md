- A Space Secret value the model writes is now redacted from the live Task
  stream even when the provider splits it across several tokens, and from the
  run's reported output and the session history stored for Continue; before,
  a value spanning streamed deltas reached stream watchers, the Task output,
  and stored history in plain text.
