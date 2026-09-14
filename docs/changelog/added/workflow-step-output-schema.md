- A Workflow `agent_task` step can declare an `output_schema` (a JSON Schema in
  the supported subset): the step's run is constrained to return a machine-readable
  answer matching it, the validated value is persisted on the run and step, and
  the step succeeds only when the answer validates — otherwise it fails.
