- A linked Telegram account now acts only while its user has signed in to
  BuildMax within `channels.sign_in_window` (default `session_absolute_ttl`);
  past it the bot asks them to sign in again, so a chat link no longer outlives
  a login or an identity-provider offboarding
  ([chat apps](https://github.com/icloudbb/buildmax/blob/main/manual/chat-apps.md#stay-signed-in)).
