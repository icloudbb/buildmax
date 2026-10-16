/**
 * Where a conversation came from, or null for Portal chat itself. Only the
 * channels that arrive from elsewhere (a chat app such as Telegram, a webhook)
 * are worth marking in a list of mostly Portal conversations.
 */
export function conversationSourceLabel(channel: string): string | null {
  if (!channel || channel === "portal") return null
  return channel.replace(/^./, (first: string) => first.toUpperCase())
}
