- A Workflow node with an `output_schema` now succeeds when its Task runs in a
  worker: the worker receives the schema, requests structured output on both
  the direct and managed model transports, and reports the validated value.
