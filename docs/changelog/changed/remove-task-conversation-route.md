- `GET /api/spaces/{space_id}/tasks/{task_id}/conversation` is removed. It has
  answered 404 "conversation file not found" for every task since sessions
  became bundles, and no Portal, Desktop, or CLI surface called it; a task's
  thread is read from its runs and each run's trace.
