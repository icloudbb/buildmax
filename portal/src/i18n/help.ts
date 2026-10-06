import type { Messages } from "@buildmax/gui"

export const helpMessages = {
  en: {
    "help.manifestError": "The help manual could not be loaded.",
    "help.pageError": "This help page could not be loaded.",
    "help.notFound": "Page not found",
    "help.back": "Back to the start of the manual",
    "help.title": "Help",
    "help.language": "Language",
    "help.contents": "Help contents",
  },
  "zh-CN": {
    "help.manifestError": "无法加载帮助手册。",
    "help.pageError": "无法加载此帮助页面。",
    "help.notFound": "未找到页面",
    "help.back": "返回手册首页",
    "help.title": "帮助",
    "help.language": "语言",
    "help.contents": "帮助目录",
  },
} satisfies Messages<string>
