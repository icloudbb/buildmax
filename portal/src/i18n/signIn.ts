import type { Messages } from "@buildmax/gui"

// Confirming a CLI or Desktop sign-in. The person "confirms" or "denies" a
// sign-in request: "approval" already names Remote Control tool approvals, and
// "device" a Remote Control viewer (design enterprise-identity-and-access §14.1).
export const signInMessages = {
  en: {
    "signIn.title": "Confirm a sign-in",
    "signIn.entry.intro": "Enter the code your BuildMax CLI or Desktop shows, to see which sign-in it is.",
    "signIn.entry.label": "Code",
    "signIn.entry.submit": "Continue",
    "signIn.loading": "Looking up the sign-in…",

    "signIn.client.cli": "BuildMax CLI",
    "signIn.client.desktop": "BuildMax Desktop",
    "signIn.request.heading": "{client} is asking to sign in to your account",
    "signIn.request.clientTerm": "Client",
    "signIn.request.unverified": "Reported by the client, not verified",
    "signIn.request.noLabel": "The client reported no name",
    "signIn.request.codeTerm": "Code",
    "signIn.request.codeHint": "Check that this matches the code your CLI or Desktop shows.",
    "signIn.request.timeTerm": "Requested",
    "signIn.request.time": "Started at {started}, expires at {expires}",
    "signIn.request.accountTerm": "Account",
    "signIn.request.sessionEnds": "The session it creates ends {ends}.",
    "signIn.request.endsSoon":
      "Your Portal session ends in less than an hour, and a session confirmed now ends with it. Sign in again first for a full-length session.",
    "signIn.request.signInAgain": "Sign in again",
    "signIn.request.warning":
      "Only confirm if you started this sign-in yourself, just now. If someone sent you this link or code, deny it.",
    "signIn.request.confirm": "Confirm sign-in",
    "signIn.request.deny": "Deny",
    "signIn.request.decisionFailed": "The decision was not recorded",

    "signIn.invalid":
      "This code is not valid any more. It may be mistyped, expired, or already confirmed or denied. Start the sign-in again from your CLI or Desktop.",
    "signIn.throttled": "Too many codes were tried from this account. Wait a few minutes, then try again.",
    "signIn.failed": "The sign-in request could not be loaded",
    "signIn.retry": "Try again",
    "signIn.enterAnother": "Enter another code",
    "signIn.confirmed":
      "Sign-in confirmed. Return to {client}: it finishes signing in on its own. You can close this page.",
    "signIn.denied":
      "Sign-in denied. {client} will report that the sign-in was not allowed. You can close this page.",
  },
  "zh-CN": {
    "signIn.title": "确认登录",
    "signIn.entry.intro": "输入 BuildMax CLI 或 Desktop 上显示的代码，查看是哪一次登录。",
    "signIn.entry.label": "代码",
    "signIn.entry.submit": "继续",
    "signIn.loading": "正在查找登录请求…",

    "signIn.client.cli": "BuildMax CLI",
    "signIn.client.desktop": "BuildMax Desktop",
    "signIn.request.heading": "{client} 请求登录你的账号",
    "signIn.request.clientTerm": "客户端",
    "signIn.request.unverified": "由客户端自行报告，未经验证",
    "signIn.request.noLabel": "客户端未报告名称",
    "signIn.request.codeTerm": "代码",
    "signIn.request.codeHint": "请核对此代码与 CLI 或 Desktop 上显示的是否一致。",
    "signIn.request.timeTerm": "请求时间",
    "signIn.request.time": "发起于 {started}，{expires} 过期",
    "signIn.request.accountTerm": "账号",
    "signIn.request.sessionEnds": "由此创建的会话于 {ends} 结束。",
    "signIn.request.endsSoon":
      "你的 Portal 会话将在一小时内结束，现在确认的会话也会随之结束。如需完整时长的会话，请先重新登录。",
    "signIn.request.signInAgain": "重新登录",
    "signIn.request.warning": "仅当这次登录是你本人刚刚发起的时才确认。如果是别人发给你的链接或代码，请拒绝。",
    "signIn.request.confirm": "确认登录",
    "signIn.request.deny": "拒绝",
    "signIn.request.decisionFailed": "未能记录此决定",

    "signIn.invalid": "此代码已失效。它可能输错了、已过期，或已被确认或拒绝。请在 CLI 或 Desktop 上重新发起登录。",
    "signIn.throttled": "此账号尝试的代码过多。请等待几分钟后再试。",
    "signIn.failed": "无法加载此登录请求",
    "signIn.retry": "重试",
    "signIn.enterAnother": "输入其他代码",
    "signIn.confirmed": "已确认登录。请回到 {client}：它会自动完成登录。你可以关闭此页面。",
    "signIn.denied": "已拒绝登录。{client} 会提示此次登录未获允许。你可以关闭此页面。",
  },
} satisfies Messages<string>
