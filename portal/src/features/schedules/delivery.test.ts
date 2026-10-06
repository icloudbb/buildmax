import { describe, expect, it } from "vitest"

import type { Translate } from "@buildmax/gui"
import type { ApiAssistant, ApiScheduleDelivery } from "../../lib/api/types"
import { translate, type MessageKey } from "../../i18n"
import { deliveringAssistants, describeDelivery } from "./delivery"

const t: Translate<MessageKey> = (key, vars) => translate("en", key, vars)

function delivery(patch: Partial<ApiScheduleDelivery>): ApiScheduleDelivery {
  return { id: "d1", schedule_id: "s1", fire_ref: "t1", status: "pending", created_at: "2026-10-04T00:00:00Z", ...patch }
}

describe("describeDelivery", () => {
  it("reads every status, and every skip reason in words", () => {
    expect(describeDelivery(undefined, t)).toBe("—")
    expect(describeDelivery(delivery({}), t)).toBe("Waiting for the run")
    expect(describeDelivery(delivery({ status: "delivered" }), t)).toBe("Sent")
    expect(describeDelivery(delivery({ status: "failed" }), t)).toBe("Could not be sent")
    expect(describeDelivery(delivery({ status: "skipped", reason: "link_inactive" }), t)).toBe(
      "Not sent: the person's chat link is gone or inactive",
    )
    // A reason a newer server added is shown as is rather than hidden.
    expect(describeDelivery(delivery({ status: "skipped", reason: "added_later" }), t)).toBe("Not sent: added_later")
  })
})

describe("deliveringAssistants", () => {
  it("keeps the assistants whose roster has the executor", () => {
    const a = (id: string, kind: "agent" | "workflow", entry: string) =>
      ({ id, roster: [{ kind, id: entry, releasable: ["answer"] }] }) as unknown as ApiAssistant
    const got = deliveringAssistants([a("a1", "agent", "ag_1"), a("a2", "workflow", "ag_1"), a("a3", "agent", "ag_2")], "agent", "ag_1")
    expect(got.map((x) => x.id)).toEqual(["a1"])
  })
})
