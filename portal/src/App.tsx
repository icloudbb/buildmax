import { useEffect, useMemo, useState } from "react"
import { AuthProvider, useAuth } from "./contexts/AuthContext"
import { ThemeProvider } from "@buildmax/gui"
import { AppProvider, useApp } from "./contexts/AppContext"
import { WebSocketProvider } from "./contexts/WebSocketContext"
import { SpaceProvider, useSpace } from "./contexts/SpaceContext"
import { Layout } from "./layout/Layout"
import { AppRouter } from "./components/AppRouter"
import { Alert } from "./components/state/Alert"
import { EmptyState } from "./components/state/EmptyState"
import { CreateSpaceDialog } from "./components/CreateSpaceDialog"
import { useConversations } from "./hooks/useConversations"
import { deriveResourceState } from "./state/resourceState"
import { Login } from "./pages/auth/Login"
import { navigate } from "./router"

function AppContent() {
  const { token, user, status, logout } = useAuth()
  const { route } = useApp()
  const { currentSpaceId, spacesState, refetchSpaces, setCurrentSpaceId } = useSpace()
  const [createSpaceOpen, setCreateSpaceOpen] = useState(false)
  const {
    data: conversationsData,
    loading: conversationsLoading,
    error: conversationsError,
    refetch: refetchConversations,
  } = useConversations(token, currentSpaceId)
  const conversations = conversationsData ?? []
  const conversationsState = useMemo(
    () =>
      deriveResourceState({
        loading: conversationsLoading,
        data: conversationsData,
        error: conversationsError,
        isEmpty: (data) => data.length === 0,
      }),
    [conversationsLoading, conversationsData, conversationsError]
  )

  useEffect(() => {
    if (!token || !currentSpaceId) return
    if (route.name !== "login") return
    navigate({ name: "chat", spaceId: currentSpaceId })
  }, [token, route, currentSpaceId])

  // The URL is authoritative for Space context (docs/design/portal-navigation
  // -and-space-context.md): reconcile the shell to whatever Space a direct
  // link or a reload just named, so the sidebar and switcher agree with what
  // is on screen. This only ever sets context, never navigates -- the one
  // place a Space change is a genuine user action, and so the one place that
  // also redirects the route, is the switcher itself (Sidebar.tsx).
  useEffect(() => {
    if (!("spaceId" in route) || !route.spaceId || route.spaceId === currentSpaceId) return
    setCurrentSpaceId(route.spaceId)
  }, [route, currentSpaceId, setCurrentSpaceId])

  if (status === "loading") {
    // Restoring the session from the refresh cookie. Render a bare loader
    // rather than the login form, which would otherwise flash on every reload
    // for an already-signed-in user.
    return <div className="app-loading">Loading…</div>
  }

  if (!token) {
    return <Login />
  }

  if (!currentSpaceId) {
    // Every Space-scoped route needs a real Space id before it can render --
    // wait for the account's Spaces to resolve (usually instant: the last
    // selected Space is read back from local storage before this ever
    // renders). A failed Space lookup, a genuinely empty account, and "still
    // loading" are distinct states, not one "No space available" catch-all --
    // see docs/design/portal-state-and-permission-feedback.md.
    return (
      <div className="app-loading">
        {spacesState.kind === "readyEmpty" ? (
          <EmptyState
            message="You don't belong to a Space yet."
            action={{ label: "Create Space", onClick: () => setCreateSpaceOpen(true) }}
          />
        ) : spacesState.kind === "error" || spacesState.kind === "forbidden" || spacesState.kind === "notFound" ? (
          <Alert
            tone={spacesState.kind}
            message={spacesState.error.message}
            retry={{ label: "Retry", onClick: () => void refetchSpaces() }}
          />
        ) : (
          "Loading…"
        )}
        <CreateSpaceDialog open={createSpaceOpen} onClose={() => setCreateSpaceOpen(false)} />
      </div>
    )
  }

  return (
    <Layout
      route={route}
      conversations={conversations}
      user={user!}
      onLogout={logout}
    >
      <AppRouter
        conversations={conversations}
        conversationsState={conversationsState}
        onRefetchConversations={refetchConversations}
        userId={user!.id}
      />
    </Layout>
  )
}

function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <SpaceProvider>
          <WebSocketProvider>
            <AppProvider>
              <AppContent />
            </AppProvider>
          </WebSocketProvider>
        </SpaceProvider>
      </AuthProvider>
    </ThemeProvider>
  )
}

export default App
