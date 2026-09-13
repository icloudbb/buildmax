import type { Conversation } from "../lib/types"
import { apiConversationToConversation } from "../lib/api/mappers"
import { getConversations } from "../features/conversations"
import type { RequestError } from "../state/resourceState"
import { useAsyncList } from "./useAsyncList"

const CONVERSATIONS_LIMIT = 100

export function useConversations(
  token: string | null,
  currentSpaceId: string | null,
  enabled = true
): { data: Conversation[] | null; loading: boolean; error: RequestError | null; refetch: () => Promise<void> } {
  return useAsyncList(
    () => {
      if (!token || !currentSpaceId) return Promise.resolve([])
      return getConversations(currentSpaceId, token, { limit: CONVERSATIONS_LIMIT }).then(
        (res) => res.conversations
      )
    },
    (list) => list.map(apiConversationToConversation),
    [token, currentSpaceId],
    enabled && !!token && !!currentSpaceId,
    { fallbackMessage: "Failed to load conversations" }
  )
}
