import Markdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { ChatComposer, ChatThread, type ChatThreadItem } from "@buildmax/gui"
import { AgentAvatar, UserAvatar } from "../../../components/UserAvatar"
import type { ApiConversationMessage, LoginUser } from "../../../lib/api"
import type { ConversationTaskCards } from "../hooks/useConversationTasks"
import { buildConversationThread } from "../thread"
import { TaskCard } from "./TaskCard"
import { Alert } from "../../../components/state/Alert"

interface ConversationDetailViewProps {
  historyRef: React.RefObject<HTMLElement | null>
  messages: ApiConversationMessage[]
  messagesLoading: boolean
  messagesError: string | null
  /** Background tasks this conversation started, shown as cards in the thread. */
  taskCards: ConversationTaskCards
  onOpenIssue?: (issueId: string) => void
  input: string
  setInput: (value: string) => void
  sending: boolean
  sendError: string | null
  streamingContent: string | null
  /** Shown after “new chat” navigation until the first user message appears from the server. */
  optimisticUserMessage: string | null
  /** Messages accepted while a turn is running, waiting for their own turn. */
  queuedMessages: string[]
  user: LoginUser | null
  onSend: () => void
}

export function ConversationDetailView({
  historyRef,
  messages,
  messagesLoading,
  messagesError,
  taskCards,
  onOpenIssue,
  input,
  setInput,
  sending,
  sendError,
  streamingContent,
  optimisticUserMessage,
  queuedMessages,
  user,
  onSend,
}: ConversationDetailViewProps) {
  // Messages and task cards are separate records ordered against each other by
  // when each was created; see thread.ts.
  const items: ChatThreadItem[] = buildConversationThread(messages, taskCards.tasks).map((entry) => {
    if (entry.kind === "task") {
      const { task } = entry
      return {
        id: task.id,
        role: "task",
        label: "Background task",
        hideAvatar: true,
        body: (
          <TaskCard
            task={task}
            busy={taskCards.busyTaskId === task.id}
            error={taskCards.actionError?.taskId === task.id ? taskCards.actionError.message : null}
            onStop={taskCards.stop}
            onRetry={taskCards.retry}
            onOpenTrace={taskCards.openTrace}
            onOpenIssue={onOpenIssue}
          />
        ),
      }
    }
    const msg = entry.message
    const isUser = msg.role === "user"
    return {
      id: msg.id,
      role: msg.role,
      label: isUser ? "You" : msg.role,
      avatar: isUser && user ? <UserAvatar user={user} size="sm" /> : <AgentAvatar size="sm" />,
      body: msg.content ? (
        <div className="page-chat__msg-content page-chat__markdown">
          <Markdown remarkPlugins={[remarkGfm]}>{msg.content}</Markdown>
        </div>
      ) : null,
    }
  })

  const showOptimisticUser =
    optimisticUserMessage &&
    !messages.some(
      (m) => m.role === "user" && m.content === optimisticUserMessage
    )

  if (showOptimisticUser) {
    items.push({
      id: "optimistic-user",
      role: "user",
      label: "You",
      avatar: user ? <UserAvatar user={user} size="sm" /> : undefined,
      body: (
        <div className="page-chat__msg-content page-chat__markdown">
          <Markdown remarkPlugins={[remarkGfm]}>{optimisticUserMessage}</Markdown>
        </div>
      ),
    })
  }

  // Queued messages sit at the end of the thread, dimmed: accepted, but not sent to
  // the model yet.
  queuedMessages.forEach((content, i) => {
    items.push({
      id: `queued-${i}`,
      role: "user",
      label: "You (queued)",
      avatar: user ? <UserAvatar user={user} size="sm" /> : undefined,
      body: (
        <div className="page-chat__msg-content page-chat__markdown page-chat__msg-content--queued">
          <Markdown remarkPlugins={[remarkGfm]}>{content}</Markdown>
        </div>
      ),
    })
  })

  if (streamingContent !== null) {
    items.push({
      id: "streaming-assistant",
      role: "assistant",
      label: "Assistant (streaming)",
      avatar: <AgentAvatar size="sm" />,
      body: (
        <div className="page-chat__msg-content page-chat__markdown">
          {streamingContent ? (
            <Markdown remarkPlugins={[remarkGfm]}>{streamingContent}</Markdown>
          ) : (
            <p className="bm-chat-thread__text bm-chat-thread__text--muted">Thinking…</p>
          )}
        </div>
      ),
    })
  }

  return (
    <div className="page-chat">
      {taskCards.tasksError ? (
        <Alert
          tone={taskCards.tasksErrorKind === "error" && taskCards.tasks.length > 0 ? "stale" : taskCards.tasksErrorKind ?? "error"}
          message={`Background tasks: ${taskCards.tasksError}`}
          retry={taskCards.tasksErrorKind === "forbidden" ? undefined : { label: "Retry tasks", onClick: taskCards.retryLoad }}
        />
      ) : null}
      <ChatThread
        historyRef={historyRef}
        ariaLabel="Conversation history"
        items={items}
        loadingText={messagesLoading ? "Loading conversation…" : null}
        errorText={messagesError}
        emptyText="No messages yet. Use the input below to start."
      />

      <section className="page-chat__input" aria-label="Send a message">
        <ChatComposer
          value={input}
          onChange={setInput}
          onSubmit={onSend}
          loading={sending}
          error={sendError}
          placeholder="Type a message… (Enter to send, Shift+Enter for new line)"
          queueWhileLoading
          queuePlaceholder="Type a message… (Enter to queue it for the next turn)"
          ariaLabel="Message"
        />
      </section>
    </div>
  )
}
