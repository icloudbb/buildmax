- Conversation turns — Portal chat, chat apps, and WebSocket — now appear in the
  managed call ledger with surface `conversation`, and their tokens count toward
  the conversation's Space usage and token quota; before, a turn's model calls
  cost money but were missing from Space usage, the admin LLM calls view, and
  quota, so a Space over its token limit could keep chatting.
