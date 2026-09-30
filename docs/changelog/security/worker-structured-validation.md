- The server now checks a worker-reported structured value against the Task's
  own `output_schema` before storing it, and drops a value that does not
  conform, so a buggy or compromised worker cannot hand a Workflow node an
  off-schema value; before, the value was stored exactly as reported.
