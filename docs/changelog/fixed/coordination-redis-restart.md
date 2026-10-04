- After the coordination Redis restarted, two server replicas could both keep a
  chat bot's receive lease and answer each message twice, so the first link
  code a chat app sent no longer worked; and a conversation could refuse its
  next few turns. Leases and their fencing tokens now survive Redis losing its data.
