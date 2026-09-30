- A Schedule whose runs are admitted and then always fail now counts those
  failures and pauses after the fifth, the same as one whose firings cannot
  start; the pause reason reads `consecutive_failures`.
