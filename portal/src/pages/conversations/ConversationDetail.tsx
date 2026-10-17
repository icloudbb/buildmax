import { useAuth } from "../../contexts/AuthContext"
import {
  ConversationDetailView,
  useConversationDetail,
  useConversationTasks,
} from "../../features/conversations"
import { RunTraceModal } from "../../features/runs"
import { navigate } from "../../router"
import { useConversationLabel } from "../../layout/Breadcrumbs"
import type { Conversation } from "../../lib/types"

interface ConversationDetailProps {
  spaceId: string
  conversationId: string
  /** The Space's conversation list entry, once it has loaded; it names the page. */
  conversation?: Conversation
  onRefetch?: () => void
  initialMessage?: string
}

export function ConversationDetail({
  spaceId,
  conversationId,
  conversation,
  onRefetch,
  initialMessage,
}: ConversationDetailProps) {
  const { token, user } = useAuth()
  const conversationLabel = useConversationLabel()
  const taskCards = useConversationTasks({
    spaceId,
    conversationId,
    token,
  })
  const conversationDetail = useConversationDetail({
    spaceId,
    conversationId,
    token,
    initialMessage,
    onMessageSent: onRefetch,
    scrollSignal: taskCards.tasks,
  })

  return (
    <>
      <ConversationDetailView
        title={conversationLabel(conversation)}
        historyRef={conversationDetail.historyRef}
        messages={conversationDetail.messages}
        messagesLoading={conversationDetail.messagesLoading}
        messagesError={conversationDetail.messagesError}
        taskCards={taskCards}
        onOpenIssue={(issueId) => navigate({ name: "issue", spaceId, issueId })}
        input={conversationDetail.input}
        setInput={conversationDetail.setInput}
        sending={conversationDetail.sending}
        sendError={conversationDetail.sendError}
        streamingContent={conversationDetail.streamingContent}
        optimisticUserMessage={conversationDetail.optimisticUserMessage}
        queuedMessages={conversationDetail.queuedMessages}
        user={user}
        onSend={conversationDetail.handleSend}
      />
      <RunTraceModal
        open={taskCards.traceRunId != null}
        spaceId={spaceId}
        token={token}
        taskRunId={taskCards.traceRunId}
        onClose={taskCards.closeTrace}
      />
    </>
  )
}
