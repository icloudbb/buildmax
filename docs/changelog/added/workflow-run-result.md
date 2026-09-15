- A workflow definition may declare a `result` selector (a `source` and RFC 6901
  `pointer` into a step's output, like an input binding); a succeeding run resolves
  it once and stores it as the run's authoritative result, surfaced on the run
  detail and on the issue the run belongs to.
