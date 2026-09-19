- Creating an Issue with a status, owner, or executor is now one request:
  `POST /api/spaces/{space_id}/issues` accepts `status`, `owner_id`,
  `executor_kind`, and `executor_id`, and a refused value creates nothing, so
  Portal no longer leaves a half-configured Issue behind a failed create.
