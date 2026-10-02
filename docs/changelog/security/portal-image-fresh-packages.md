- The Portal image is now built from freshly upgraded Alpine packages on every
  release instead of a cached layer, so it ships pcre2 10.49 (CVE-2026-103111)
  and picks up later Alpine security fixes without a manual cache purge.
