- An agent working a space issue now reads and reports on it by running
  `buildmax issue show` and `buildmax issue comment` rather than through the
  built-in GetIssue and ReportToIssue tools, which have been removed; issue
  length and per-run comment limits are now enforced on the server for every
  client alike.
