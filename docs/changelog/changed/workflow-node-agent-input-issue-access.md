- A workflow node now names its Agent under `agent` (`{"id": …}`) and its task
  under `input` (`{"instruction": …, "bindings": […]}`) instead of the flat
  `target_agent_id`, `prompt`, and `bindings` fields, and it declares an
  `issue_access` mode. `none` (the default) gives the node's Task no Issue
  relation; `if_bound` attaches the run's Issue when it has one; `required`
  additionally refuses to start a run that has no Issue. This makes each node's
  Issue capability an explicit choice and replaces the earlier flat node shape;
  existing definitions must be re-authored.
