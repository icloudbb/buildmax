import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react"
import type { PortalSessionResponse, LoginUser } from "../lib/api"
import { TOKEN_REFRESHED_EVENT, UNAUTHORIZED_EVENT } from "../lib/api"
import type { TokenRefreshedDetail } from "../lib/api/client"
import { restoreSession, revokeSession } from "../features/auth/api"
import { clearAccessToken, expiresAtFrom, setAccessToken } from "../lib/api/session"
import { clearStoredCurrentSpaceId } from "../lib/storage/currentSpaceStorage"

/** "loading" until the one-shot session restore settles, then "ready". */
type AuthStatus = "loading" | "ready"

interface AuthState {
  token: string | null
  user: LoginUser | null
  /**
   * The account's deployment authority, as its session last reported it.
   * Presentation only: the admin routes check the grant themselves.
   */
  systemRoles: string[]
  status: AuthStatus
}

interface AuthContextValue extends AuthState {
  login: (res: PortalSessionResponse) => void
  logout: () => void
}

const SIGNED_OUT: AuthState = { token: null, user: null, systemRoles: [], status: "ready" }

function signedIn(res: PortalSessionResponse): AuthState {
  return { token: res.access_token, user: res.user, systemRoles: res.system_roles ?? [], status: "ready" }
}

function sameRoles(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((role, i) => role === b[i])
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ ...SIGNED_OUT, status: "loading" })

  const login = useCallback((res: PortalSessionResponse) => {
    setAccessToken(res.access_token, expiresAtFrom(res.expires_in))
    setState(signedIn(res))
  }, [])

  const logout = useCallback(() => {
    // Tell the server first: clearing local state only makes this browser
    // forget the session, while the refresh cookie stays usable for weeks.
    // Best effort — a failed call must not strand someone in a session they
    // asked to leave.
    void revokeSession()
    clearAccessToken()
    clearStoredCurrentSpaceId()
    setState(SIGNED_OUT)
  }, [])

  useEffect(() => {
    // Hydrate once from the refresh cookie the server holds. A reload starts
    // with no in-memory token, so this is the only way a returning session
    // comes back without a fresh login.
    let cancelled = false
    void (async () => {
      const res = await restoreSession()
      if (cancelled) return
      if (res) {
        setAccessToken(res.access_token, expiresAtFrom(res.expires_in))
        setState(signedIn(res))
      } else {
        setState(SIGNED_OUT)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    function onUnauthorized() {
      logout()
    }
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized)
  }, [logout])

  useEffect(() => {
    // A refresh can happen inside any request. Without this the context would
    // keep handing out the replaced token, and every consumer that reconnects
    // on a token change — the WebSocket above all — would keep using the old
    // one until the next reload. The exchange also reports the account's
    // current system roles, which is how a grant or revocation reaches the
    // shell without a reload.
    function onRefreshed(event: Event) {
      const detail = (event as CustomEvent<Partial<TokenRefreshedDetail>>).detail
      const accessToken = detail?.accessToken
      if (!accessToken) return
      const systemRoles = detail.systemRoles ?? []
      setState((prev) =>
        prev.token === accessToken && sameRoles(prev.systemRoles, systemRoles)
          ? prev
          : { ...prev, token: accessToken, systemRoles }
      )
    }
    window.addEventListener(TOKEN_REFRESHED_EVENT, onRefreshed)
    return () => window.removeEventListener(TOKEN_REFRESHED_EVENT, onRefreshed)
  }, [])

  const value: AuthContextValue = {
    ...state,
    login,
    logout,
  }

  return (
    <AuthContext.Provider value={value}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error("useAuth must be used within AuthProvider")
  return ctx
}
