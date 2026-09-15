- A workflow definition now describes a graph of `nodes` joined by `needs`
  edges instead of an ordered `steps` array: a node becomes ready when every
  node it needs has succeeded, so dependencies — not list position — decide the
  order. Publication rejects a graph that is not acyclic, a `needs` edge to a
  missing node, or an input binding that reads a node which is not one of its
  predecessors. Execution stays fail-fast (one node's failure blocks the rest)
  and dispatches one ready node at a time for now. This replaces the earlier
  `steps`/`step_id` shape; existing definitions must be re-authored as
  `nodes`/`id`.
