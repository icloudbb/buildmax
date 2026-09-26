- The production reference and `./make ocean deploy` now configure the
  deployment key-encryption key (`secret.kek_file`, mounted from the
  `buildmax-kek` Secret), so they can store managed-model credentials and Space
  Secrets; `ocean deploy` generates it once in the state directory. Every
  deployment, kind included, now mounts the key read-only with mode `0400` at
  `/etc/buildmax/kek/kek.json`, outside `BUILDMAX_HOME`. The configuration and
  deployment guides say how to generate it and that it must be backed up apart
  from the database, since losing it makes sealed credentials unreadable.
