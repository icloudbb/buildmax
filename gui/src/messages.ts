import { createTranslator, type Messages } from "./i18n"

// Strings rendered by the shared components themselves. A surface that passes
// its own label overrides these.
export const guiMessages = {
  en: {
    "gui.close": "Close",
    "gui.cancel": "Cancel",
    "gui.theme.toDark": "Switch to dark mode",
    "gui.theme.toLight": "Switch to light mode",
    "gui.theme.dark": "Dark mode",
    "gui.theme.light": "Light mode",
    "gui.composer.placeholder": "Type a message… (Enter to send, Shift+Enter for new line)",
    "gui.composer.message": "Message",
    "gui.composer.send": "Send",
    "gui.composer.sending": "Sending…",
    "gui.composer.stop": "Stop",
    "gui.composer.queue": "Queue",
    "gui.question.title": "Question",
    "gui.question.tab": "Q{n}",
    "gui.question.ownAnswer": "Or type your own answer…",
    "gui.question.answer": "Type your answer…",
    "gui.question.answerLabel": "Your answer",
    "gui.question.confirm": "Confirm",
    "gui.question.send": "Send",
    "gui.question.dismiss": "Dismiss",
  },
  "zh-CN": {
    "gui.close": "关闭",
    "gui.cancel": "取消",
    "gui.theme.toDark": "切换到深色模式",
    "gui.theme.toLight": "切换到浅色模式",
    "gui.theme.dark": "深色模式",
    "gui.theme.light": "浅色模式",
    "gui.composer.placeholder": "输入消息…（Enter 发送，Shift+Enter 换行）",
    "gui.composer.message": "消息",
    "gui.composer.send": "发送",
    "gui.composer.sending": "发送中…",
    "gui.composer.stop": "停止",
    "gui.composer.queue": "排队",
    "gui.question.title": "问题",
    "gui.question.tab": "问题 {n}",
    "gui.question.ownAnswer": "或输入你自己的回答…",
    "gui.question.answer": "输入你的回答…",
    "gui.question.answerLabel": "你的回答",
    "gui.question.confirm": "确认",
    "gui.question.send": "发送",
    "gui.question.dismiss": "忽略",
  },
} satisfies Messages<string>

export const { useT: useGuiT } = createTranslator(guiMessages)
