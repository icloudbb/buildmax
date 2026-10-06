import type { Messages } from "@buildmax/gui"

// The sign-in page, which renders before any Space or account exists.
export const authMessages = {
  en: {
    "auth.loading": "Loading…",
    "auth.genericProvider": "single sign-on",
    "auth.signInWith": "Sign in with {provider}",
    "auth.signInToContinue": "Sign in to continue",
    "auth.signInWithCode": "Sign in with a login code from your administrator",
    "auth.or": "or",
    "auth.email": "Email",
    "auth.password": "Password",
    "auth.loginCode": "Login code",
    "auth.signIn": "Sign in",
    "auth.useCode": "Forgot your password, or have a login code?",
    "auth.usePassword": "Sign in with a password",
    "auth.codeFooter": "Ask an administrator for a code. Once you are in, set a password from account settings.",
    "auth.signInFailed": "Sign in failed",

    "auth.sso.unavailable": "Single sign-on is temporarily unavailable. Please try again in a moment.",
    "auth.sso.expired": "That sign-in attempt expired or was interrupted. Please try again.",
    "auth.sso.notAuthorized": "Your account is not authorized for this deployment. Contact your administrator.",
    "auth.sso.disabled": "This account is disabled. Contact your administrator.",
  },
  "zh-CN": {
    "auth.loading": "加载中…",
    "auth.genericProvider": "SSO",
    "auth.signInWith": "使用 {provider} 登录",
    "auth.signInToContinue": "登录以继续",
    "auth.signInWithCode": "使用管理员提供的登录码登录",
    "auth.or": "或",
    "auth.email": "邮箱",
    "auth.password": "密码",
    "auth.loginCode": "登录码",
    "auth.signIn": "登录",
    "auth.useCode": "忘记密码，或已有登录码？",
    "auth.usePassword": "使用密码登录",
    "auth.codeFooter": "请向管理员索取登录码。登录后，可在账户设置中设置密码。",
    "auth.signInFailed": "登录失败",

    "auth.sso.unavailable": "单点登录暂时不可用，请稍后重试。",
    "auth.sso.expired": "此次登录已过期或被中断，请重试。",
    "auth.sso.notAuthorized": "你的账户未获授权访问此部署，请联系管理员。",
    "auth.sso.disabled": "此账户已被停用，请联系管理员。",
  },
} satisfies Messages<string>
