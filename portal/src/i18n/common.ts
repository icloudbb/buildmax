import type { Messages } from "@buildmax/gui"

// Shared presenters and fallbacks: state alerts, load-failure pages, the
// not-found page, dates, and the generic error text hooks fall back to.
export const commonMessages = {
  en: {
    "common.cancel": "Cancel",
    "common.copy": "Copy",
    "common.copied": "Copied",
    "common.copyFailed": "Copy failed",
    "common.tryAgain": "Try again",
    "common.requestFailed": "Request failed",
    "common.downloadFailed": "Download failed",

    "common.alert.error": "Something went wrong",
    "common.alert.forbidden": "Access denied",
    "common.alert.notFound": "Not found",
    "common.alert.stale": "Showing previous data",

    "common.unavailable.notFound":
      "No {resource} with this reference exists. It may have been deleted, or the link may be wrong.",
    "common.unavailable.forbidden":
      "This {resource} belongs to a Space you don't have access to. The Space may not exist, or you may not be a member.",
    "common.unavailable.error": "This {resource} could not be loaded.",

    "common.notFound.title": "Page not found",
    "common.notFound.body": "Nothing here matches this address. It may be mistyped, or a link from before a rename.",
    "common.notFound.back": "Back to Chat",

    "common.space.createIntro": "Create a new shared space for agents, workflows, issues, and conversations.",
    "common.space.namePlaceholder": "e.g. Design, Ops, Research",
    "common.space.createFailed": "Failed to create space",
    "common.space.loadFailed": "Failed to load spaces",
    "common.space.membershipFailed": "Failed to load membership",
    "common.conversations.loadFailed": "Failed to load conversations",

    "common.history.current": "Current: v{revision}",
    "common.history.loading": "Loading history…",
    "common.history.empty": "No history recorded yet.",
    "common.history.restore": "Restore",

    "common.time.today": "Today {time}",
    "common.time.yesterday": "Yesterday {time}",
  },
  "zh-CN": {
    "common.cancel": "取消",
    "common.copy": "复制",
    "common.copied": "已复制",
    "common.copyFailed": "复制失败",
    "common.tryAgain": "重试",
    "common.requestFailed": "请求失败",
    "common.downloadFailed": "下载失败",

    "common.alert.error": "出错了",
    "common.alert.forbidden": "无权访问",
    "common.alert.notFound": "未找到",
    "common.alert.stale": "正在显示之前的数据",

    "common.unavailable.notFound": "不存在对应此引用的 {resource}。它可能已被删除，或链接有误。",
    "common.unavailable.forbidden":
      "此 {resource} 属于你无权访问的 Space。该 Space 可能不存在，或你不是其成员。",
    "common.unavailable.error": "无法加载此 {resource}。",

    "common.notFound.title": "未找到页面",
    "common.notFound.body": "没有与此地址匹配的内容。地址可能有误，或是重命名之前的链接。",
    "common.notFound.back": "返回对话",

    "common.space.createIntro": "创建一个新的共享 Space，用于管理 Agent、Workflow、Issue 和对话。",
    "common.space.namePlaceholder": "例如：设计、运维、研究",
    "common.space.createFailed": "创建 Space 失败",
    "common.space.loadFailed": "加载 Space 失败",
    "common.space.membershipFailed": "加载成员身份失败",
    "common.conversations.loadFailed": "加载对话失败",

    "common.history.current": "当前：v{revision}",
    "common.history.loading": "正在加载历史…",
    "common.history.empty": "暂无历史记录。",
    "common.history.restore": "恢复",

    "common.time.today": "今天 {time}",
    "common.time.yesterday": "昨天 {time}",
  },
} satisfies Messages<string>
