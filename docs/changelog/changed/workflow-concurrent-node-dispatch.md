- A workflow run now dispatches every ready node at once instead of one at a
  time, so independent branches of the graph execute in parallel. A definition
  may cap the parallelism with `policy.max_parallel_nodes` (1 to the deployment
  maximum); absent, a run uses the deployment ceiling. Failure stays fail-fast:
  one node's failure now also cancels the siblings that were running alongside
  it and ends the run.
