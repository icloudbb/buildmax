- A managed model call that hits the model's `call_timeout` is now recorded as
  a failed `upstream_timeout` call and logged, not as a cancellation; a refused
  provider key and provider rate limiting get their own `upstream_auth_failed`
  and `upstream_rate_limited` codes, a task run failed by the managed gateway
  is classed `model` (or by who must act) instead of `run`, and a refused
  catalog key names `model set-key` instead of `settings.yaml`.
