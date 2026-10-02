- A worker run that needs a choice only the user can make now asks through
  AskUser far more reliably, so its Task shows it is waiting for an answer
  instead of ending with the question in its reply: the deferred AskUser tool
  and prompt state that a question written in the reply is never delivered
  (GPT-5.6 Luna: 1 of 5 runs before, 16 of 18 after).
