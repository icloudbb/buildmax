- The end-of-turn recap now costs far fewer tokens: it spends a model call only
  when the turn actually changed something and did not already describe it —
  read-only and self-explaining turns are skipped — and when it does run, the
  file bodies a write or edit carried are no longer sent to be summarised.
