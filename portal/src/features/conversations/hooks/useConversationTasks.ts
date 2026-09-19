import { useCallback, useEffect, useRef, useState } from "react"
import { useFetch } from "../../../hooks/useFetch"
import { useWebSocket } from "../../../contexts/WebSocketContext"
import { SOCKET_OPEN_EVENT } from "../../../lib/api/ws"
import { getErrorMessage } from "../../../lib/errorMessage"
import type { ApiTask } from "../../../lib/api/types"
import type { RequestErrorKind } from "../../../state/resourceState"
import { cancelTask, getTasks, retryTask } from "../../tasks/api"

interface UseConversationTasksOptions {
  spaceId: string | null
  conversationId: string
  token: string | null
}

interface MessageCompletedPayload {
  conversation_id?: string
}

export interface ConversationTaskCards {
  tasks: ApiTask[]
  tasksError: string | null
  tasksErrorKind: RequestErrorKind | null
  retryLoad: () => void
  /** The task whose stop or retry is in flight, if any. */
  busyTaskId: string | null
  actionError: { taskId: string; message: string } | null
  traceRunId: string | null
  stop: (taskId: string) => void
  retry: (taskId: string) => void
  openTrace: (taskRunId: string) => void
  closeTrace: () => void
}

/**
 * The conversation's task cards, read from the server rather than accumulated
 * from events.
 *
 * Everything that changes a card — a turn starting one, a worker finishing one,
 * someone else retrying one — arrives as an invalidation, and the answer to all
 * of them is to read the tasks again. That is what makes the cards survive a
 * refresh and a reconnect: the socket says something changed, the database says
 * what it changed to, and a missed event costs one stale card until the next
 * event rather than a card that never appears at all.
 */
export function useConversationTasks({
  spaceId,
  conversationId,
  token,
}: UseConversationTasksOptions): ConversationTaskCards {
  const ws = useWebSocket()
  const {
    data: tasks,
    error: tasksError,
    errorKind: tasksErrorKind,
    refetch,
  } = useFetch(() => getTasks(spaceId!, conversationId, token!), [spaceId, conversationId, token], {
    enabled: !!(token && spaceId && conversationId),
    errorMessage: (e) => getErrorMessage(e, "Failed to load tasks"),
  })

  const [busyTaskId, setBusyTaskId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<{ taskId: string; message: string } | null>(null)
  const [traceRunId, setTraceRunId] = useState<string | null>(null)

  const refetchRef = useRef(refetch)
  refetchRef.current = refetch

  useEffect(() => {
    setBusyTaskId(null)
    setActionError(null)
    setTraceRunId(null)
  }, [conversationId])

  useEffect(() => {
    const reload = () => refetchRef.current()
    // A turn is where a task is created, so a finished turn may have added one
    // this conversation has never seen.
    const onCompleted = (payload: MessageCompletedPayload) => {
      if (payload?.conversation_id !== conversationId) return
      reload()
    }
    // The event names a task, not a conversation, so any of the space's tasks
    // changing reloads this conversation's. The alternative is keeping a
    // task-to-conversation map on the client and trusting it to be complete.
    const onTaskStatus = () => reload()

    ws.on("task.status.changed", onTaskStatus)
    ws.on("conversation.message.completed", onCompleted)
    ws.on(SOCKET_OPEN_EVENT, reload)
    return () => {
      ws.off("task.status.changed", onTaskStatus)
      ws.off("conversation.message.completed", onCompleted)
      ws.off(SOCKET_OPEN_EVENT, reload)
    }
  }, [ws, conversationId])

  const runAction = useCallback(
    (taskId: string, action: (spaceId: string, taskId: string, token: string) => Promise<unknown>, failed: string) => {
      if (!spaceId || !token) return
      setBusyTaskId(taskId)
      setActionError(null)
      action(spaceId, taskId, token)
        .catch((err) => setActionError({ taskId, message: getErrorMessage(err, failed) }))
        .finally(() => {
          setBusyTaskId(null)
          refetchRef.current()
        })
    },
    [spaceId, token]
  )

  const stop = useCallback(
    (taskId: string) => runAction(taskId, cancelTask, "Failed to stop the run"),
    [runAction]
  )
  const retry = useCallback(
    (taskId: string) => runAction(taskId, retryTask, "Failed to run it again"),
    [runAction]
  )

  return {
    tasks: tasks ?? [],
    tasksError,
    tasksErrorKind,
    retryLoad: refetch,
    busyTaskId,
    actionError,
    traceRunId,
    stop,
    retry,
    openTrace: setTraceRunId,
    closeTrace: () => setTraceRunId(null),
  }
}
