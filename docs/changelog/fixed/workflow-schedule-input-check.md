- A Workflow Schedule whose fixed input its Workflow cannot accept, or whose
  Workflow has a step that requires an Issue, is now refused when it is saved
  (HTTP 400) instead of being accepted and then failing every firing until it
  paused itself.
