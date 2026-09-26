- "Allow session" on `BrowserNavigate` now covers one origin instead of every
  site, the approval prompt names that origin, and `BrowserClick` and
  `BrowserType` refuse a page that a link or redirect took to an origin not
  opened with `BrowserNavigate`.
