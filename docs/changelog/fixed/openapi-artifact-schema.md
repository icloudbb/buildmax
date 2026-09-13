- The OpenAPI documents now define the `Artifact` schema every artifact route's
  response referenced, so `/openapi.json` (and the worker document) no longer
  carry a dangling `$ref` for the file a create or read returns.
