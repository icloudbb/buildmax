- The kind reference cluster now runs Cilium instead of kindnet, so
  NetworkPolicy is enforced in the kernel and a long-lived local cluster no
  longer drifts into slow DNS and unprotected new pods. Existing clusters keep
  kindnet until recreated with `./make kind down` and `./make kind up`.
