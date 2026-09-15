- Workflow step input bindings now select a value from a source (`workflow.input`
  or an earlier step's `node.<id>.output` envelope of text, structured output, and
  Artifact references) at an RFC 6901 JSON Pointer, instead of injecting an
  earlier step's whole output; the step editor authors a source and pointer per
  input.
