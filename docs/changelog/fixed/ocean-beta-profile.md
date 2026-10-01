- `./make ocean deploy` now matches the Beta profile and runs Agents: worker
  runs call the model through the managed gateway instead of failing with
  "model not found", two Server replicas run on separate nodes coordinated
  through a Server-only Redis, and the DOKS cluster pins a version slug so a
  later `ocean up` change such as resizing the node pool no longer fails.
