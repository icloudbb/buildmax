- Replace a catalog model's provider key in place with
  `buildmax admin model set-key <id>`, `buildmax-server model set-key --id`, or
  `PUT /api/admin/llm/models/{model_id}/credential`. The model keeps its name, so
  no client changes what it selects. `--api-key -` on either `model add` reads
  the key from standard input, and `./make kind seed` refreshes the keys of
  models it seeded before and skips example placeholder keys.
