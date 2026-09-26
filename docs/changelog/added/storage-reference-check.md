- `buildmax-server storage verify [--checksums]` checks, read-only, that every
  artifact, checkpoint payload, run trace, and plugin package the database
  names is present in storage, lists any that are missing or altered by id, and
  exits non-zero if it finds one, so a restored database and bucket can be
  proven to agree.
