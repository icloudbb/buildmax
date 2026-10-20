import type { Translate } from "@buildmax/gui"
import { describe, expect, it } from "vitest"
import { translate, type MessageKey } from "../../i18n"
import {
  dependencyLabel,
  dependencyStatusLabel,
  formatCount,
  orderedRunStatuses,
  quotaTierLabel,
  workerRunModeLabel,
  workerTransportLabel,
} from "./labels"
import { systemRoleLabel } from "./roles"

const en: Translate<MessageKey> = (key, vars) => translate("en", key, vars)
const zh: Translate<MessageKey> = (key, vars) => translate("zh-CN", key, vars)

describe("Administration labels", () => {
  it("names each server value in both languages", () => {
    expect(dependencyLabel("object_storage", en)).toBe("Object storage")
    expect(dependencyLabel("database", zh)).toBe("数据库")
    expect(dependencyStatusLabel("ok", en)).toBe("Healthy")
    expect(dependencyStatusLabel("failed", zh)).toBe("异常")
    expect(systemRoleLabel("system_admin", en)).toBe("System Administrator")
    expect(systemRoleLabel("system_admin", zh)).toBe("系统管理员")
    expect(workerRunModeLabel("k8s_job", en)).toBe("Kubernetes Job")
    expect(workerRunModeLabel("local_process", zh)).toBe("本地进程")
    expect(workerTransportLabel("direct", en)).toBe("Direct to the model provider")
    expect(workerTransportLabel("buildmax", zh)).toBe("经由服务器的模型网关")
    expect(quotaTierLabel("free_trial", en)).toBe("Free trial")
    expect(quotaTierLabel("pro", zh)).toBe("专业版")
  })

  it("humanizes a value this build does not know rather than showing it raw", () => {
    expect(dependencyLabel("search_index", en)).toBe("Search index")
    expect(quotaTierLabel("standard", en)).toBe("Standard")
  })

  it("groups digits for the locale", () => {
    expect(formatCount(10_000_000, "en")).toBe("10,000,000")
    expect(formatCount(10_000_000, "zh-CN")).toBe("10,000,000")
    expect(formatCount(258, "en")).toBe("258")
  })

  it("orders run statuses as the work moves, unknown last", () => {
    expect(
      orderedRunStatuses({ SUCCEEDED: 4, CANCELED: 1, RUNNING: 2, MYSTERY: 1, PENDING: 3 }).map(([s]) => s),
    ).toEqual(["PENDING", "RUNNING", "SUCCEEDED", "CANCELED", "MYSTERY"])
  })
})
