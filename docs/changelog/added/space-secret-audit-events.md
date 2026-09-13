- Creating, disabling, or destroying a space Secret now writes a
  `secret.created`, `secret.disabled`, or `secret.destroyed` event to the space
  audit trail, with the Secret's id as the target and no item name, value, or
  ciphertext in the event.
