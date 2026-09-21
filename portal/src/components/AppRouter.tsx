import { useEffect } from "react"
import type { Conversation } from "../lib/types"
import type { ResourceState } from "../state/resourceState"
import { useApp } from "../contexts/AppContext"
import { AgentList } from "../pages/agents/AgentList"
import { AgentDetail } from "../pages/agents/AgentDetail"
import { ConversationDetail } from "../pages/conversations/ConversationDetail"
import { TaskDetail } from "../pages/tasks/TaskDetail"
import { NewConversation } from "../pages/conversations/NewConversation"
import { Explore } from "../pages/explore/Explore"
import { Issues } from "../pages/issues/Issues"
import { IssueDetail } from "../pages/issues/IssueDetail"
import { Artifacts } from "../pages/artifacts/Artifacts"
import { ArtifactDetail } from "../pages/artifacts/ArtifactDetail"
import { Workflows } from "../pages/workflows/Workflows"
import { SchedulesPage } from "../pages/schedules/SchedulesPage"
import { WorkflowDetail } from "../pages/workflows/WorkflowDetail"
import { WorkflowRunDetail } from "../pages/workflows/WorkflowRunDetail"
import { AccountSettings } from "../pages/settings/AccountSettings"
import { Marketplace } from "../pages/marketplace/Marketplace"
import { RemoteControlList } from "../pages/remoteControl/RemoteControlList"
import { RemoteControlSession } from "../pages/remoteControl/RemoteControlSession"
import { Help } from "../pages/help/Help"
import { SpaceSettings } from "../pages/settings/SpaceSettings"
import { AdminSettings } from "../pages/admin/AdminSettings"
import { NotFoundPage } from "../pages/errors/NotFoundPage"

export interface AppRouterProps {
  conversations: Conversation[]
  conversationsState: ResourceState<Conversation[]>
  onRefetchConversations: () => Promise<void>
  userId: string
}

export function AppRouter({
  conversations,
  conversationsState,
  onRefetchConversations,
  userId,
}: AppRouterProps) {
  const {
    route,
    token,
    pendingConversation,
    setPendingConversation,
  } = useApp()

  const routeConversationId = route.name === "chat" ? route.conversationId : undefined

  useEffect(() => {
    if (!pendingConversation) return
    const viewing = route.name === "chat" && routeConversationId === pendingConversation.conversationId
    if (!viewing) setPendingConversation(null)
  }, [route.name, routeConversationId, pendingConversation, setPendingConversation])

  if (route.name === "chat") {
    if (route.conversationId) {
      const initialMessage =
        pendingConversation?.conversationId === route.conversationId
          ? pendingConversation.initialMessage
          : undefined
      return (
        <ConversationDetail
          spaceId={route.spaceId}
          conversationId={route.conversationId}
          onRefetch={onRefetchConversations}
          initialMessage={initialMessage}
        />
      )
    }
    return (
      <NewConversation
        token={token ?? undefined}
        spaceId={route.spaceId}
        onRefetchConversations={onRefetchConversations}
        conversations={conversations}
        conversationsState={conversationsState}
      />
    )
  }

  if (route.name === "explore") return <Explore spaceId={route.spaceId} />

  if (route.name === "agents") {
    return (
      <AgentList
        token={token ?? null}
        spaceId={route.spaceId}
      />
    )
  }

  if (route.name === "agent") {
    return <AgentDetail token={token ?? null} spaceId={route.spaceId} agentId={route.agentId} />
  }

  if (route.name === "account") return <AccountSettings section={route.section ?? "general"} />
  if (route.name === "space")
    return <SpaceSettings spaceId={route.spaceId} section={route.section ?? "overview"} />
  if (route.name === "admin")
    return <AdminSettings section={route.section ?? "overview"} userId={route.userId} />

  if (route.name === "workflows") {
    return <Workflows token={token ?? null} spaceId={route.spaceId} />
  }

  if (route.name === "workflow") {
    return <WorkflowDetail token={token ?? null} spaceId={route.spaceId} workflowId={route.workflowId} />
  }

  if (route.name === "workflowRun") {
    return (
      <WorkflowRunDetail token={token ?? null} spaceId={route.spaceId} workflowRunId={route.workflowRunId} />
    )
  }

  if (route.name === "schedules") {
    return <SchedulesPage token={token ?? null} spaceId={route.spaceId} />
  }

  if (route.name === "issues") {
    return <Issues token={token ?? null} spaceId={route.spaceId} userId={userId} />
  }

  if (route.name === "issue") {
    return <IssueDetail token={token ?? null} spaceId={route.spaceId} issueId={route.issueId} userId={userId} />
  }

  if (route.name === "artifacts") {
    return <Artifacts spaceId={route.spaceId} />
  }

  if (route.name === "marketplace") {
    return <Marketplace token={token ?? null} />
  }

  if (route.name === "remoteControl") {
    return <RemoteControlList token={token ?? null} />
  }

  if (route.name === "remoteControlSession") {
    return <RemoteControlSession token={token ?? null} sessionId={route.sessionId} />
  }

  if (route.name === "help") {
    return <Help slug={route.slug} />
  }

  if (route.name === "artifact") {
    return <ArtifactDetail artifactId={route.artifactId} />
  }

  if (route.name === "task") {
    return <TaskDetail token={token ?? null} spaceId={route.spaceId} taskId={route.taskId} />
  }

  if (route.name === "notFound") {
    return <NotFoundPage />
  }

  // A safety net, not a route this ever intentionally reaches: parseHash
  // never emits an unhandled name, so this only fires if a route name is
  // added to the Route union without a case here -- rendering a visible
  // not-found is what catches that mistake instead of silently showing Chat.
  return <NotFoundPage />
}
