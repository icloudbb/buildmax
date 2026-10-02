- A browser click that submits a form or follows a link now returns the page
  it navigated to. It used to read the location before the navigation began,
  so the Agent was shown the page it had just left and kept its now-stale
  element references.
