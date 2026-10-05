import { describe, expect, it } from "vitest"
import type { Translate } from "@buildmax/gui"
import type { ApiRunProvenance } from "../../lib/api/types"
import { translate, type MessageKey } from "../../i18n"
import {
  describeAgent,
  describeOrigin,
  describeSpaceInstructions,
  inputMatchesMessage,
  runInputLabel,
} from "./origin"

// The English catalog, which every expectation below is written against.
const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function provenance(over: Partial<ApiRunProvenance> = {}): ApiRunProvenance {
  return {
    task_run_id: "tr_1",
    task_id: "tk_1",
    status: "SUCCEEDED",
    input: "investigate the flaky test",
    created_at: "1970-01-01T00:16:40Z",
    ...over,
  }
}

describe("describeOrigin", () => {
  it("names the path that put the run in flight", () => {
    expect(describeOrigin(provenance({ trigger_source: "workflow_step" }), t).text).toContain(
      "workflow step"
    )
    expect(describeOrigin(provenance({ trigger_source: "portal_conversation" }), t).text).toContain(
      "conversation"
    )
  })

  it("says an unrecorded path is unrecorded rather than inventing one", () => {
    expect(describeOrigin(provenance(), t).text).toContain("unrecorded")
  })

  it("separates a missing quote that is expected from one that is a gap", () => {
    // A retry carries no new instruction, so nothing was said.
    expect(describeOrigin(provenance({ trigger_source: "task_retry" }), t).quote).toBe("none-expected")
    // A conversation run should name a message. Not having one is a gap.
    expect(describeOrigin(provenance({ trigger_source: "portal_conversation" }), t).quote).toBe(
      "none-recorded"
    )
  })

  it("marks a run that repeats another, however it was labelled", () => {
    expect(describeOrigin(provenance({ trigger_source: "task_retry" }), t).isRepeat).toBe(true)
    expect(describeOrigin(provenance({ retry_of_task_run_id: "tr_0" }), t).isRepeat).toBe(true)
    expect(describeOrigin(provenance({ trigger_source: "portal_conversation" }), t).isRepeat).toBe(
      false
    )
  })

  it("says when a runtime rather than a person started the run", () => {
    expect(
      describeOrigin(provenance({ trigger_source: "workflow_step", created_by_type: "system" }), t).text
    ).toContain("not a person")
    expect(
      describeOrigin(provenance({ trigger_source: "webhook", created_by_type: "webhook" }), t).text
    ).toContain("external caller")
  })

  it("reports a quoted message as quoted", () => {
    const described = describeOrigin(
      provenance({
        trigger_source: "portal_conversation",
        source_message: { id: "cm_1", content: "look into it", truncated: false, created_at: "1970-01-01T00:00:01Z" },
      }),
      t,
    )
    expect(described.quote).toBe("quoted")
  })
})

describe("inputMatchesMessage", () => {
  it("is true only when Tier 1 passed the request through unchanged", () => {
    expect(
      inputMatchesMessage(
        provenance({
          input: "look into it",
          source_message: { id: "cm_1", content: "look into it", truncated: false, created_at: "1970-01-01T00:00:01Z" },
        })
      )
    ).toBe(true)
    expect(
      inputMatchesMessage(
        provenance({
          input: "investigate the flaky test",
          source_message: {
            id: "cm_1",
            content: "look into the flaky test, but leave CI alone",
            truncated: false,
            created_at: "1970-01-01T00:00:01Z",
          },
        })
      )
    ).toBe(false)
  })

  it("never claims a match on a truncated quote, which is not the whole text", () => {
    expect(
      inputMatchesMessage(
        provenance({
          input: "same",
          source_message: { id: "cm_1", content: "same", truncated: true, created_at: "1970-01-01T00:00:01Z" },
        })
      )
    ).toBe(false)
  })

  it("is false when there is nothing to compare", () => {
    expect(inputMatchesMessage(provenance())).toBe(false)
  })
})

describe("describeAgent", () => {
  it("says nothing when the run had no agent", () => {
    expect(describeAgent(provenance(), t)).toBeNull()
  })

  it("names the revision the run actually executed under", () => {
    const described = describeAgent(
      provenance({ agent: { id: "ag_1", name: "Reviewer", revision: 3, current_revision: 3 } }),
      t,
    )
    expect(described?.text).toContain("Reviewer, revision 3")
    expect(described?.driftedSinceRun).toBe(false)
  })

  it("flags a definition that has been edited since the run", () => {
    const described = describeAgent(
      provenance({ agent: { id: "ag_1", name: "Reviewer", revision: 3, current_revision: 5 } }),
      t,
    )
    expect(described?.driftedSinceRun).toBe(true)
    expect(described?.text).toContain("revision 5")
  })

  it("says an unrecorded revision is unrecorded rather than showing the current one", () => {
    const described = describeAgent(
      provenance({ agent: { id: "ag_1", name: "Reviewer", revision: 0, current_revision: 5 } }),
      t,
    )
    expect(described?.text).toContain("not recorded")
    expect(described?.text).not.toContain("revision 5")
  })

  it("still names a deleted agent, and says it is gone", () => {
    const described = describeAgent(
      provenance({ agent: { id: "ag_1", name: "Retired", revision: 1, current_revision: 1, deleted: true } }),
      t,
    )
    expect(described?.text).toContain("Retired")
    expect(described?.text).toContain("deleted")
  })

  it("falls back to the handle when the definition could not be named", () => {
    expect(describeAgent(provenance({ agent: { id: "ag_1", revision: 2 } }), t)?.text).toContain("ag_1")
  })
})

describe("describeSpaceInstructions", () => {
  it("says nothing when an older run has no Space instruction provenance", () => {
    expect(describeSpaceInstructions(provenance(), t)).toBeNull()
  })

  it("names the revision handed to the worker", () => {
    const described = describeSpaceInstructions(
      provenance({ space_instructions: { revision: 3, current_revision: 3 } }),
      t,
    )
    expect(described?.text).toContain("revision 3")
    expect(described?.driftedSinceRun).toBe(false)
  })

  it("flags Space instructions edited after the run", () => {
    const described = describeSpaceInstructions(
      provenance({ space_instructions: { revision: 2, current_revision: 5 } }),
      t,
    )
    expect(described?.text).toContain("revision 5")
    expect(described?.driftedSinceRun).toBe(true)
  })

  it("distinguishes a run with no configured instructions from missing provenance", () => {
    const unchanged = describeSpaceInstructions(
      provenance({ space_instructions: { revision: 0, current_revision: 0 } }),
      t,
    )
    expect(unchanged?.text).toContain("No Space instructions")
    expect(unchanged?.driftedSinceRun).toBe(false)

    const configuredLater = describeSpaceInstructions(
      provenance({ space_instructions: { revision: 0, current_revision: 1 } }),
      t,
    )
    expect(configuredLater?.text).toContain("now revision 1")
    expect(configuredLater?.driftedSinceRun).toBe(true)
  })
})

describe("runInputLabel", () => {
  it("credits a Portal- or directly-triggered run to the signed-in person", () => {
    for (const trigger of [
      "task_create",
      "task_rerun",
      "task_retry",
      "portal_conversation",
      "portal_task_create",
      "portal_task_rerun",
    ]) {
      expect(runInputLabel({ trigger_source: trigger, created_by_type: "user" }, t)).toBe("You")
    }
  })

  it("never credits a workflow- or issue-dispatched run to a person", () => {
    expect(runInputLabel({ trigger_source: "workflow_step", created_by_type: "user" }, t)).toBe(
      "Workflow"
    )
    expect(runInputLabel({ trigger_source: "issue_agent_run", created_by_type: "user" }, t)).toBe(
      "Issue"
    )
  })

  it("names the runtime or an external caller instead of guessing a person", () => {
    expect(runInputLabel({ created_by_type: "system" }, t)).toBe("System")
    expect(runInputLabel({ trigger_source: "webhook", created_by_type: "webhook" }, t)).toBe("Webhook")
  })

  it("says 'Unknown origin' for migrated data rather than defaulting to You", () => {
    expect(runInputLabel({}, t)).toBe("Unknown origin")
  })
})
