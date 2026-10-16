import type { Messages } from "@buildmax/gui"

// The section at the top of the Issues page: the work in this Space that is
// waiting on the signed-in person.
export const needsMeMessages = {
  en: {
    "needsMe.title": "Waiting on you",
    "needsMe.count": { one: "{count} item", other: "{count} items" },
    "needsMe.empty": "Nothing in this Space is waiting on you. New work starts as an Issue.",
    "needsMe.empty.action": "Create an Issue",
    "needsMe.error": "Failed to load what is waiting on you",
    "needsMe.more": "Showing {shown} of {total}",

    "needsMe.issues.title": "Your Issues that need attention",
    "needsMe.awaiting.title": "Runs waiting for your answer",
    "needsMe.requests.title": "Workflow requests to answer",
    "needsMe.failed.title": "Failed runs",

    "needsMe.reason.runFailed": "Its latest run failed",
    "needsMe.reason.awaitingAnswer": "Its latest run is waiting for your answer",
    "needsMe.reason.workflowRequest": "Its Workflow is waiting on a request",
    "needsMe.request.input": "Step {step} needs input",
    "needsMe.request.question": "Step {step} asked a question",
    "needsMe.failed.fallback": "The run failed",

    "needsMe.open.issue": "Open Issue",
    "needsMe.open.answer": "Answer",
    "needsMe.open.respond": "Respond",
    "needsMe.open.run": "Open run",
  },
  "zh-CN": {
    "needsMe.title": "等待你处理",
    "needsMe.count": "{count} 项",
    "needsMe.empty": "此 Space 中没有等待你处理的工作。新的工作从 Issue 开始。",
    "needsMe.empty.action": "创建一个 Issue",
    "needsMe.error": "加载等待你处理的工作失败",
    "needsMe.more": "显示 {total} 项中的 {shown} 项",

    "needsMe.issues.title": "需要你关注的 Issue",
    "needsMe.awaiting.title": "等待你回答的运行",
    "needsMe.requests.title": "需要回答的 Workflow 请求",
    "needsMe.failed.title": "失败的运行",

    "needsMe.reason.runFailed": "最新运行失败",
    "needsMe.reason.awaitingAnswer": "最新运行正在等待你的回答",
    "needsMe.reason.workflowRequest": "其 Workflow 正在等待一个请求",
    "needsMe.request.input": "步骤 {step} 需要输入",
    "needsMe.request.question": "步骤 {step} 提出了问题",
    "needsMe.failed.fallback": "运行失败",

    "needsMe.open.issue": "打开 Issue",
    "needsMe.open.answer": "回答",
    "needsMe.open.respond": "响应",
    "needsMe.open.run": "打开运行",
  },
} satisfies Messages<string>
