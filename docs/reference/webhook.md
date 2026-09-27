# Webhook Reference

> **简体中文：** [阅读中文镜像](../zh-CN/reference/webhook.md)
> **Audience:** operators and integrators · **Status:** current
An external system can start a BuildMax run by sending one HTTP POST. The
request is mapped straight to a conversation turn and a background task run —
no Tier 1 LLM call is spent deciding what to do with it.

## Create a Key

Webhook keys are **user-scoped**, not space-scoped, and are managed through the
authenticated user API:

| Method | Route |
|---|---|
| `POST` | `/api/webhook-keys` |
| `GET` | `/api/webhook-keys` |
| `DELETE` | `/api/webhook-keys/{key_id}` |

Keys are stored as a SHA-256 hash. **The plaintext is returned only once, at
creation.** Copy it immediately; it cannot be retrieved later. Revoke a leaked
key by deleting it.

## Call the Endpoint

```http
POST /api/webhook
Authorization: Bearer <webhook-key>
Content-Type: application/json

{ "message": "Summarize yesterday's error log" }
```

`X-Webhook-Key: <webhook-key>` works as an alternative to the `Authorization`
header. There is no id in the path — the key itself resolves the owning user.

### Request Body

The prompt is read from a configurable JSON path, so you can point the webhook
at a payload shape you do not control:

```yaml
# <BUILDMAX_HOME>/server.yaml
webhook:
  message_path: message      # e.g. "body.text" for a nested field
```

The run belongs to the key's owner and starts a new conversation in their
personal Space; the payload cannot name another identity.

### Responses

| Status | Meaning |
|---|---|
| `202 Accepted` | Run created. Body is `{"task_id": "...", "task_run_id": "..."}`. |
| `400 Bad Request` | The body could not be mapped to a turn — usually `message_path` does not match the payload. |
| `401 Unauthorized` | Missing or invalid key. |
| `409 Conflict` | The target task already has a run in progress. |
| `503 Service Unavailable` | Webhooks or webhook keys are not configured on this server. |

Use the returned `task_id` to poll run status or to correlate with the run
afterwards.

## Notes

- The run executes exactly like any other task run: a worker materializes the
  files, runs the shared agent runtime, and writes artifacts.
- Because the key identifies a user, everything the run touches is scoped by
  that user's space membership.
- Treat a webhook key as a credential that can spend LLM budget and execute
  tools. Rotate it the same way you would an API token.
