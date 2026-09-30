- Over-long or whitespace-only names, titles, descriptions, and inputs now
  return a 400 with a clear message instead of a 500: Space/Agent/Secret/
  Schedule/Workflow/Issue/webhook-key names, Secret/Issue/Workflow descriptions,
  and Task/Schedule inputs are bounded to their storage columns.
