/**
 * The share of runs that succeeded, among those that either succeeded or
 * failed. A stopped run is left out: someone chose to end it, which says
 * nothing about whether the Agent works. Null when no run succeeded or failed,
 * so an Agent whose only finished runs were stopped shows no rate rather than
 * 0%.
 *
 * Statuses are the server's, in either case the Portal carries them.
 */
export function successRate(statuses: string[]): number | null {
  let succeeded = 0
  let failed = 0
  for (const status of statuses) {
    const normalized = status.toUpperCase()
    if (normalized === "SUCCEEDED" || normalized === "SUCCESS") succeeded++
    else if (normalized === "FAILED") failed++
  }
  return succeeded + failed === 0 ? null : succeeded / (succeeded + failed)
}

/** A rate as a whole percentage, or "—" when there is none. */
export function formatSuccessRate(rate: number | null): string {
  return rate == null ? "—" : `${Math.round(rate * 100)}%`
}
