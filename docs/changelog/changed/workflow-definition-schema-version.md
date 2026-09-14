- A Workflow definition must now declare `"schema_version": 1`, and may declare
  an `input_schema` and a `result` selector; publication rejects an unknown
  version, an input schema outside the supported subset, or a result naming a
  step that does not exist.
