- `./make kind up` works again after MinIO withdrew its images: the kind
  deployment now pulls the MinIO server and `mc` from SILO, the community MinIO
  fork (`pgsty/minio`, `pgsty/mc`), pinned by tag and digest.
