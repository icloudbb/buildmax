- Disabling an account that is already disabled keeps its original
  `disabled_at` instead of moving the deactivation time forward each time
  cleanup is re-run.
