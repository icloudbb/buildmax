- Desktop: destructive confirmations — deleting a project with sessions,
  clearing a project's sessions, and removing a Git-checkout plugin — now use an
  in-app dialog. They previously relied on the native `window.confirm`, which the
  webview can silently drop, leaving the action unconfirmable.
