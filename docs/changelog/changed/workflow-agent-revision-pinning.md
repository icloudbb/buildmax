- Publishing a workflow now pins each node's Agent to a specific revision:
  a node that names no `agent.revision` is pinned to the Agent's current
  revision, and the pinned number is stored in the published definition. A run
  started later snapshots that revision's content, so editing an Agent no longer
  changes what an already-published plan runs. Publication rejects a node that
  pins a revision the Agent never had.
