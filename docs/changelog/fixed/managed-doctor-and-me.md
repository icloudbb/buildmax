- `buildmax doctor` checks the mode first and no longer fails a signed-in setup
  for lacking `settings.yaml`, probes unused local models, or suggests
  `buildmax login` for an outage. `buildmax me` asks the deployment, so a
  revoked login is no longer shown as signed in. Signed-out hints now mention
  `buildmax login`, and an unknown model name says which deployment was asked.
