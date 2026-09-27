- The agent can now ask you questions and wait for your answers in the TUI and
  Desktop: the new `AskUser` tool asks up to four questions at once, each
  answered by picking an option, checking several, or typing your own answer
  under the question, or you can dismiss them. A `Notification`
  hook fires with `user_question` while it waits.
