- Validating a Plugin now warns when its `hooks.yaml` wraps events in a
  settings-style `hooks:` block, which silently contributes no hooks, instead of
  accepting it with no sign anything is wrong.
