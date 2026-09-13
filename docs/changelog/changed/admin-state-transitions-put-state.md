- Admin stored-flag transitions are now a single idempotent `PUT .../state`
  instead of paired POST actions: `PUT /api/admin/users/{user_id}/state`
  (`disabled`), `PUT /api/admin/llm/models/{model_id}/state` (`enabled`),
  `PUT /api/admin/plugins/{plugin_name}/state` (`archived`), and
  `PUT /api/admin/plugins/{plugin_name}/releases/{version}/state` (`yanked`).
  The old `/disable`, `/enable`, `/archive`, `/unarchive`, and `/yank` routes no
  longer exist; clients that ship with the server were updated in step.
