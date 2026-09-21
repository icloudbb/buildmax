- Desktop: deleting a scheduled task now works. The confirmation moved from the
  native `window.confirm`, which the webview could silently drop, to an in-app
  dialog that spells out the deletion is irreversible, and the Delete button
  reads as a danger action.
