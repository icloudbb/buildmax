import type { ApiSecretGrantProblem } from "../../lib/api/types"

/**
 * How many of an Agent's Secret grants would fail a run started now. An
 * optional grant is skipped rather than failing the run, so it does not count.
 * The server reports the problems to every role that can read the Agent; see
 * docs/design/space-secrets.md §10.
 */
export function blockingGrantCount(problems: ApiSecretGrantProblem[] | undefined): number {
  return (problems ?? []).filter((problem) => !problem.optional).length
}
