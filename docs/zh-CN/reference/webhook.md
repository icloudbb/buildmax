# Webhook 参考

> **翻译说明：** 本文是[英文原文](../../reference/webhook.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员和集成开发者 · **状态：** 当前
外部系统发送一个 HTTP POST 即可启动 BuildMax 运行。请求直接映射为一个 Conversation 回合和一次后台 TaskRun，无需消耗 Tier 1 LLM 调用来决定如何处理。

## 创建密钥

Webhook 密钥**以用户为作用域**，不是以 Space 为作用域，通过已认证用户 API 管理：

| 方法 | 路由 |
|---|---|
| `POST` | `/api/webhook-keys` |
| `GET` | `/api/webhook-keys` |
| `DELETE` | `/api/webhook-keys/{key_id}` |

密钥以 SHA-256 哈希存储。**明文仅在创建时返回一次。** 请立即复制，之后无法再次获取。删除即可撤销泄露的密钥。

## 调用端点

```http
POST /api/webhook
Authorization: Bearer <webhook-key>
Content-Type: application/json

{ "message": "Summarize yesterday's error log" }
```

也可使用 `X-Webhook-Key: <webhook-key>` 替代 `Authorization` 请求头。路径中没有 ID，密钥本身会解析出所属用户。

### 请求体

提示词从可配置的 JSON 路径读取，因此可将 webhook 指向你无法控制格式的载荷：

```yaml
# <BUILDMAX_HOME>/server.yaml
webhook:
  message_path: message      # e.g. "body.text" for a nested field
```

运行属于密钥所有者，并在其个人 Space 中开启一个新对话；载荷无法指定其他身份。

### 响应

| 状态 | 含义 |
|---|---|
| `202 Accepted` | 已创建运行。响应体为 `{"task_id": "...", "task_run_id": "..."}`。 |
| `400 Bad Request` | 请求体无法映射为回合，通常是 `message_path` 与载荷不匹配。 |
| `401 Unauthorized` | 密钥缺失或无效。 |
| `409 Conflict` | 目标 Task 已有正在执行的运行。 |
| `503 Service Unavailable` | 此服务器未配置 webhook 或 webhook 密钥。 |

使用返回的 `task_id` 轮询运行状态，或在之后关联该运行。

## 说明

- 运行与其他 TaskRun 的执行方式完全相同：Worker 物化文件，运行共享 Agent 运行时，并写入 Artifact。
- 由于密钥标识用户，运行接触的所有内容都受该用户的 Space 成员资格约束。
- Webhook 密钥能够消耗 LLM 预算并执行工具，应将其视为凭证，像 API 令牌一样轮换。
