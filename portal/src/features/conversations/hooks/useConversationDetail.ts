import { useCallback, useEffect, useRef, useState } from "react"
import { useFetch } from "../../../hooks/useFetch"
import { getConversationMessages } from "../api"
import {
  useConversationBusy,
  useWebSocket,
} from "../../../contexts/WebSocketContext"
import { useSpace } from "../../../contexts/SpaceContext"
import { useStableT } from "../../../i18n"

interface UseConversationDetailOptions {
  spaceId: string | null
  conversationId: string
  token: string | null
  initialMessage?: string
  onMessageSent?: () => void
  /**
   * Anything else drawn in the thread. The history scrolls to the bottom when
   * it changes, so a task card arriving is not left below the fold.
   */
  scrollSignal?: unknown
}

interface MessageDeltaPayload {
  conversation_id: string
  delta: string
}

interface MessageCompletedPayload {
  conversation_id: string
  queued_remaining?: number
}

interface MessageDequeuedPayload {
  conversation_id: string
  content: string
}

interface ConversationErrorPayload {
  conversation_id?: string
  error: string
  code?: string
}

export function useConversationDetail({
  spaceId,
  conversationId,
  token,
  initialMessage,
  onMessageSent,
  scrollSignal,
}: UseConversationDetailOptions) {
  const historyRef = useRef<HTMLElement | null>(null)
  const ws = useWebSocket()
  const { busy: sending, markBusy, queued: queuedMessages } = useConversationBusy(conversationId)
  const { currentSpaceId } = useSpace()
  const stableT = useStableT()
  const {
    data: messagesData,
    loading: messagesLoading,
    error: messagesError,
    refetch: refetchMessages,
  } = useFetch(
    () => getConversationMessages(spaceId!, conversationId, token!),
    [spaceId, conversationId, token],
    {
      enabled: !!(token && spaceId && conversationId),
      errorMessage: (e) => (e instanceof Error ? e.message : stableT("chat.error.loadMessages")),
    }
  )

  const [input, setInput] = useState("")
  const [sendError, setSendError] = useState<string | null>(null)
  const [streamingContent, setStreamingContent] = useState<string | null>(null)
  const [optimisticUserMessage, setOptimisticUserMessage] = useState<string | null>(null)
  // Records the conversationId we've already sent the initial message for.
  // Keying by conversationId prevents an async space-id resolve (or any unrelated rerun
  // of the reset effect below) from wiping the guard and causing a duplicate send.
  const initialMessageSentForRef = useRef<string | null>(null)

  const refetchMessagesRef = useRef(refetchMessages)
  refetchMessagesRef.current = refetchMessages
  const onMessageSentRef = useRef(onMessageSent)
  onMessageSentRef.current = onMessageSent

  useEffect(() => {
    setInput("")
    setSendError(null)
    setStreamingContent(null)
    setOptimisticUserMessage(null)
  }, [conversationId, currentSpaceId])

  useEffect(() => {
    const handleDelta = (payload: MessageDeltaPayload) => {
      if (payload.conversation_id !== conversationId) return
      setStreamingContent((prev) => (prev ?? "") + payload.delta)
    }

    const handleCompleted = (payload: MessageCompletedPayload) => {
      if (payload.conversation_id !== conversationId) return
      setStreamingContent(null)
      refetchMessagesRef.current()
      onMessageSentRef.current?.()
    }

    // A queued message starting its own turn: it becomes the message being answered.
    const handleDequeued = (payload: MessageDequeuedPayload) => {
      if (payload.conversation_id !== conversationId) return
      setSendError(null)
      setOptimisticUserMessage(payload.content)
      setStreamingContent("")
    }

    const handleError = (payload: ConversationErrorPayload) => {
      if (payload.conversation_id && payload.conversation_id !== conversationId) return
      setSendError(payload.error)
      // A refused message leaves the running turn alone; only a failed turn clears
      // the streaming reply and the message it was answering.
      if (payload.code === "queue_full") return
      setStreamingContent(null)
      setOptimisticUserMessage(null)
    }

    ws.on("conversation.message.delta", handleDelta)
    ws.on("conversation.message.completed", handleCompleted)
    ws.on("conversation.message.dequeued", handleDequeued)
    ws.on("conversation.error", handleError)

    return () => {
      ws.off("conversation.message.delta", handleDelta)
      ws.off("conversation.message.completed", handleCompleted)
      ws.off("conversation.message.dequeued", handleDequeued)
      ws.off("conversation.error", handleError)
    }
  }, [ws, conversationId])

  const sendMessage = useCallback(
    (content: string) => {
      if (!token) return
      setSendError(null)
      // While a turn is running the server queues this message and echoes it back
      // as conversation.message.queued, which is what puts it on screen. Claiming
      // it as the message being answered here would show the wrong one.
      if (!sending) {
        setStreamingContent("")
        setOptimisticUserMessage(content)
      }
      markBusy()
      ws.send("conversation.message", {
        conversation_id: conversationId,
        content,
      })
    },
    [ws, conversationId, token, markBusy, sending]
  )

  // Send the initial message exactly once per conversation. Guarding by conversationId
  // (not a boolean reset by another effect) means a rerender from an unrelated context
  // change cannot cause a duplicate send.
  useEffect(() => {
    if (!initialMessage || !token) return
    if (initialMessageSentForRef.current === conversationId) return
    initialMessageSentForRef.current = conversationId
    sendMessage(initialMessage)
  }, [initialMessage, token, sendMessage, conversationId])

  // Clear optimistic bubble once the real message list includes it.
  useEffect(() => {
    const msgs = messagesData?.messages
    if (!optimisticUserMessage || !msgs?.length) return
    if (msgs.some((m) => m.role === "user" && m.content === optimisticUserMessage)) {
      setOptimisticUserMessage(null)
    }
  }, [messagesData?.messages, optimisticUserMessage])

  useEffect(() => {
    const el = historyRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [messagesData?.messages, streamingContent, optimisticUserMessage, queuedMessages, scrollSignal])

  function handleSend() {
    const content = input.trim()
    if (!content || !token) return
    // No busy guard: a message sent during a turn is queued, not refused.
    setInput("")
    sendMessage(content)
  }

  return {
    historyRef,
    messages: messagesData?.messages ?? [],
    messagesLoading,
    messagesError,
    input,
    setInput,
    sending,
    sendError,
    streamingContent,
    optimisticUserMessage,
    queuedMessages,
    handleSend,
  }
}
