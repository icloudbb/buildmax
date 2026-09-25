- A local CLI or Desktop session stays in the mode of its first turn. Resuming it
  after `buildmax login` or `buildmax logout` is refused rather than replaying
  its history to a destination it never used; start a new session instead.
