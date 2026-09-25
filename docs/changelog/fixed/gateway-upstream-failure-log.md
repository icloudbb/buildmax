- The managed gateway logs a provider's failure reason on the server, with the
  call's ledger ID, so an operator can tell a bad key from a provider outage.
  Callers still see only the stable error class. A client whose server is
  unreachable now says so instead of reporting the gateway "refused the call".
