- The Kubernetes reference deployment reads the OIDC client secret from
  `buildmax-secret` and trusts extra CA certificates from an optional
  `buildmax-trust` ConfigMap, for an IdP behind a private CA
  ([authentication](https://github.com/icloudbb/buildmax/blob/main/docs/deploy/authentication.md#single-sign-on-oidc)).
