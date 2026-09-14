- A Workflow run now takes an immutable input validated against the workflow's
  `input_schema` at admission and frozen onto the run; the Portal generates a run
  input form from that schema, and a workflow without one runs with no input as
  before.
