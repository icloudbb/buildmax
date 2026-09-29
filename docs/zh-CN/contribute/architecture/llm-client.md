# LLM 客户端

> **翻译说明：** 本文是[英文原文](../../../contribute/architecture/llm-client.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效

## 用途

`internal/infra/llm` 基于 BuildMax 所使用的四种 LLM 线协议，实现了 `llm.LLMClient` 契约。它在 BuildMax 类型与各协议的线格式之间做转换，并拥有让一次真实网络调用得以存活下来的一切：超时、重试、错误分类和用量采集。

**契约**位于 `internal/core/llm`；本包只是它的一种实现。Agent 循环看到的始终只是这个接口。

| `Config.Provider` | 协议 | 适配器 |
|---|---|---|
| `openai_compatible`（默认） | OpenAI Chat Completions | `openai_chat.go` |
| `openai` | OpenAI Responses | `openai_responses.go` |
| `anthropic` | Anthropic Messages | `anthropic.go` |
| `ollama` | Ollama `/api/chat`（本地） | `ollama.go`、`ollama_inventory.go` |

这四个取值就是 `internal/core/llm` 中的 `llm.Provider*`，此外还有 `Providers`、`KnownProvider` 和 `ProviderNeedsCredential`。协议名只从 `settings.yaml` 中读取一次，存入模型目录，并记录进调用账本，因此它只有一处定义，而不是每个界面各定义一份。`config.KnownLLMProvider` 只额外承担配置边界自身的解读：未设置的值意味着使用默认值。

`client.go` 是唯一的入口点：它选择一个适配器，并拥有调用方所依赖的各个部分——单次调用超时、重试循环和错误分类——从而不让四种协议在这些方面各行其是。一个适配器只负责执行一次尝试，仅此而已。

设计理由以及本阶段之后的规划：[design/llm-provider-adapters.md](../../design/LLM提供商适配器.md) 与 [design/local-ollama-provider.md](../../design/本地Ollama提供商.md)。

## 它实现的契约

```go
// internal/core/llm
type LLMClient interface {
    ChatCompletionBlocking(ctx, req Request) (Completion, error)
    ChatCompletionStreaming(ctx, req Request, onDelta func(string)) (Completion, error)
    ContextWindow() int   // 0 = no windowing configured
}

type Request struct {
    Messages []Message
    Tools    []ToolDef
    Profile  CallProfile      // what the call is for
}

type Completion struct {
    Content       string
    ToolCalls     []ToolCall
    Usage         Usage
    ProviderState *ProviderState   // reasoning state, when the protocol has any
}
```

`Message`、`ToolDef`、`ToolCall`、`Usage`、`Completion` 和 `ProviderState` 全部定义在 `internal/core/llm` 中——既不在本包，也不在 `internal/core/*` 的各个领域包中，那些包持有的是领域实体和仓储契约。

`Completion` 是一个结构体，而不是一份更长的返回值列表，因为这份契约每获得一项新能力，就想要多占一个位置，而到了第五个位置参数时，可读性就已经维持不下去了。`Completion.AssistantMessage()` 就是一个回合最终变成的历史记录条目，因此 Agent 循环会原样追加它，中间的任何一层都不需要知道推理状态的存在。

`Request` 之所以也是一个结构体，是入口方向上对称的原因。它的存在是为了携带 `CallProfile`：这次调用*是为了什么*，这一点请求本身无法体现。一次标题生成，和一次长时间工具调用运行的第一回合，发送的消息形状是一样的，但提示缓存对它们的计费方式不同——一次缓存写入比普通输入更贵，只有当后续调用真的读取它时才能回本。profile 就是调用方对“之后还会不会有人读到这份前缀”这个问题给出的答案。

| Profile | 设置方 |
|---|---|
| `agent_turn` | `core/agent.RunLoop`——该前缀会在下一次迭代中再次发出 |
| `title` | `agentapp.SessionManager.GenerateTitle` |
| `compaction` | `agentapp.LLMCompactor` 与笔记检查点器 |
| `evaluation` | 某个测试框架针对一次运行发起的“关于”调用，而不是“作为”该运行本身的调用 |
| `probe` | 不会被复用的单次提问：`WebFetch`、hook 的模型调用 |

它是一个有类型的字段，而不是 `context.Context` 中的一个值，因为这项会产生计费影响的行为，必须对需要据此推理的调用方和测试可见。`CallProfile.Valid()` 会拒绝未知取值，而不是回退到某个默认值：它本会回退到的那个默认值，恰恰是要花钱的那一个。

## 构造

```go
client, err := llm.NewClient(llm.Config{
    Provider:      m.Provider,        // "" = openai_compatible
    APIKey:        m.APIKey,
    BaseURL:       m.APIURL,
    Model:         m.Model,
    Surface:       "cli",            // cli, desktop, server, or worker
    ContextWindow: m.ContextWindow,   // 0 = no windowing
    MaxTokens:     m.MaxTokens,       // 0 = the adapter's own default
    CallTimeout:   d,                 // 0 = DefaultCallTimeoutSecs
})
```

`Config` 是本包自己的结构体，其内容来自 `settings.yaml` 中的一条 `models:` 条目、`server.yaml` 中的 `conversation.model` 块，或是由 `internal/service/llmgateway` 解析出的目录条目（catalog target）。当 `ContextWindow` 为零时，`lookupContextWindow` 会回退到一张内置的已知模型尺寸表——这张表以 OpenRouter 风格的标识符为键，因此原生模型 ID 通常需要显式设置 `context_window`。Ollama 这个提供方是例外：它会转而去问守护进程，因为本地守护进程能就它实际持有的模型给出答案。

未知的提供方会报错，而不是被兜底处理：一个无法按其配置方式访问的模型，会在选择阶段就失败，而不是把它的提示词发到运维人员从未指定的地方。

每一次对提供方的请求，都会在其 `User-Agent` 中把 BuildMax 标识为 `buildmax/<version> (<surface>)`。这个 surface 由运行时自身决定，而非用户可配置：CLI、Desktop、托管 Server 和各个 worker 会分别发送各自的来源标识。托管网关会保留原始的 CLI、Desktop 或 worker surface，并追加 `; gateway`，因此它发往上游的请求读起来会是，例如，`buildmax/0.1.0 (cli; gateway)`。

## 归一化历史记录

规范历史记录只有一种宽松的形状：一条 system 消息、若干 user 与 assistant 回合，以及每个结果各一条 `role: "tool"` 消息。每个适配器都会把它转换成自己协议下的有效请求，其中 Anthropic 适配器承担了大部分工作——它把 system 消息提升到顶层参数中，把连续出现的一串工具结果合并成一条 user 消息，丢弃结果已被裁剪掉的工具调用、以及调用已被裁剪掉的结果，跳过空文本，并补上协议要求的 `max_tokens`。

这些修补有意留在适配器这一层。如果让 `core/llm`、`TrimHistory` 或压缩逻辑去强制执行最严格那个协议的规则，就等于让另外两个协议为它们本不具备的约束买单。

Responses 适配器以**无状态**方式运行：每次调用都发送完整输入，并设置 `store: false`。历史记录、裁剪、压缩和 Session 持久化都由 BuildMax 自己拥有，服务端保存的 Conversation 状态会与这四者相互竞争。

Ollama 适配器承担另一项修补。它的协议没有工具调用标识符：一个结果是按工具名称来作答的，因此适配器会把每个 `ToolCallID` 与它前面那条助手消息中的调用做匹配，并丢弃调用已经不存在的结果。返回方向的标识符则按其在对话中的位置铸造——`call_<n>` 会接着请求中已有的编号继续往后编——因此，对于确实按标识符配对的协议而言，它写下的 Session 依然是无歧义的。

## 本地上下文窗口

Ollama 协议在提示词过长时会默默截断，而不是拒绝，因此它的适配器会在**每一次**调用中都发送 `num_ctx`，并且这个数字与 `ContextWindow()` 所报告的完全一致。设置了 `context_window` 的条目由该值决定；未设置的条目则采用守护进程针对该模型给出的答案，并以 `config.DefaultContextWindow` 为上限——因为一个模型训练时的完整长度，可能超出这台机器所能分配的内存。探测失败时会回退到默认值并记录日志——它唯独不能做的，是把这个字段整个漏掉。

`ollama_inventory.go` 服务的是诊断，而非运行本身：`OllamaInventory` 列出已拉取的内容，`OllamaShow` 报告某个模型的窗口大小与能力，这正是 `buildmax doctor` 和 `buildmax models --local` 所读取的内容。

## 推理状态

`Config.Reasoning` 是一个强度级别——`off`、`low`、`medium`、`high`——除 off 外的任何级别，都会在之后的回合中回放这次推理。Anthropic 会在对应强度下获得自适应的扩展思考（extended thinking），并带上 `display: omitted`；Responses 则获得该强度加上 `include: ["reasoning.encrypted_content"]`，这是在服务端不保存任何状态的情况下，回放推理的唯一方式。Chat Completions 没有这种状态，会忽略该设置。无法识别的级别会让 `NewClient` 直接失败，而不会发送到提供方。

返回的内容会作为 `ProviderState` 记录在助手消息上，这是一段不透明的负载，并标记着产生它的协议。由此得到三条性质，每一条都是承重的：

- **它绝不会在自己的适配器之外被读取。** 有一个签名覆盖着这段内容，因此篡改它比直接丢弃它更糟。
- **它绝不会变成 content。** 思考不是答案；把它放进对话记录会让它和结论变得无法区分。
- **带有陌生标记的负载会被丢弃，而不是被发送。** 正是这一点让一个 Session 能够在提供方之间保持可移植，同时又能携带那些原本并不可移植的状态：换到另一种协议下继续运行，只会丢失推理的连续性，仅此而已。

无法编码的状态会被整个丢弃，而不是写一半；已保存但如今无法解析的负载，会被当作完全没有状态来回放。这两种情况下，该回合都会在没有连续性的状态下继续，而这恰恰就是一个不支持推理的协议本来就会有的行为。

## 提示缓存

`Config.CacheControl` 是目标的策略——`auto`（默认）、`off` 或 `force`，外加一个保留期限——而 `Request.Profile` 则是单次调用的用途。`resolveCacheDecision` 把这两者与协议自身的能力结合起来，只有最终结果才会进入请求。

profile 补上的是配置无法提供的那一半。在 `auto` 下，只有 `agent_turn` 才会请求缓存：它的前缀会在下一次迭代中再次发出，这正是缓存写入的计费所针对的场景。而标题生成、压缩摘要或一次探测，都是问一次就不会再用同一前缀问第二次，因此为它们买一次写入纯属亏本。这个构建版本无法识别的 profile，会被当作复用情况未知处理，不足以支撑一次写入；真的想为此付费的调用方，需要显式指定 `force`。

能力属于目标，而一个直连条目所能依据的只有它自己的提供方：

| 提供方 | 请求端控制项 | 报告为 | 保留期限 |
|---|---|---|---|
| `anthropic` | 断点——除非请求指明位置，否则不会缓存任何内容 | `supported` | `5m`、`1h` |
| `openai` | 一个限定范围的 `prompt_cache_key`；Responses 无论如何都会自行缓存 | `supported` | `24h` |
| `openai_compatible` | 无——说这门协议，不等于承诺实现它的缓存字段 | `unsupported` | — |
| `ollama` | 无——本地运行时会复用自己的缓存 | `unsupported` | — |

保留期限的用词是按协议各自定义的。`5m` 和 `1h` 对 Anthropic 有意义，对 Responses API 则毫无意义；`24h` 则反过来。因此双方都会拒绝对方的取值，而不是把它原样传过去、任其被忽略。

在没有请求端控制项的目标上使用 `force`，会在构造阶段就被拒绝：把它当成完全不缓存来处理，等于回答了一个没人问过的问题。`auto` 在任何地方都会被接受，因为大多数目标都是这种情况，报错会让默认模式变得无法使用。协议未文档化的保留期限，出于同样的理由被拒绝——与其让某个字段被悄悄换成别的期限提供服务，不如给出一个明确命名的失败。

### OpenAI 缓存键

Responses API 无论是否被要求，都会进行缓存，因此 BuildMax 发送的这个键并不会“打开”缓存——它决定的是哪些前缀会被放在一起查找。这就使它成为一个正确性问题，而非安全问题；出错时的表现是，一堆彼此永不匹配的提示词共用了同一个桶，也就是一个永远不会命中的桶。

`deriveCacheKey` 只对那些必须全部匹配、命中才有可能发生的要素做哈希：凭证、模型、调用方的作用域，以及系统提示词和工具定义（按发送顺序）的指纹。各字段都以长度分隔，因此同一段字节的两种不同切法不会发生冲突；整体还带有一个版本前缀，因此改变了推导方式的构建版本，不会与未改变的版本共用同一个桶。

不会放入任何可能无端泄露或造成碎片化的内容。凭证是被哈希过的，而不是原样携带；原始提示词、消息、工作区路径和用户名则完全不会出现。这个结果按请求逐次推导，从不持久化、记录日志或返回给任何人——它只出现在一个出站字段里，别无他处。

`Request.CacheScope` 是调用方的桶判别符。对于直连调用，它是空的，因为此时凭证本身就已经是用户自己的账号。对于托管调用，网关会根据经过身份验证的 Space 来设置它，因为被授予同一个已批准模型的多个 Space，会共用一份凭证，否则就会共用同一个桶。它绝不会从客户端接受：一个能够自行指定作用域的调用方，就有可能把矛头指向另一个 Space 的桶。

### Anthropic 断点放置

在 Anthropic 上，最终请求会携带两个断点：一个是系统提示词上的静态断点，覆盖一次运行中每次调用都相同的工具和指令；另一个是顶层的滚动断点，使下一回合读取的是完整前缀，而不仅仅是对话开始之前的那部分。后者并不会取代前者——自动向前查找只能找到此前写在滚动端点附近的前缀。如果没有系统提示词，静态断点会移到最后一个工具定义上，否则唯一可缓存的边界就会落在每回合都会变化的用户消息之后。

三种协议都会报告缓存计数，最终落在 `core/llm.Usage` 的 `CacheReadTokens` 和 `CacheWriteTokens` 上。它们是对 **`PromptTokens` 的拆分，而不是在其之外的额外累加**。Anthropic 把缓存输入单独于 `input_tokens` 之外报告，因此它的适配器会在报告 prompt 总数前把它加回去；OpenAI 系协议则已经把它计算在内。这两个方向上任何一个搞错，都会误报一次运行实际花费的成本。

此后，这些计数就沿着与 prompt、completion 总数相同的路径流转：`agent.RunStats` 在一次运行中累加它们，`agent.Event` 在 `llm_start`/`llm_end` 上携带实时数字，JSONL 轨迹在 `llm_end` 和 `run_end` 上记录它们，Session 文件保存按 Session 汇总的总数，`agentapp.RunResult` 和 `RunUsage` 再把它们交给 CLI、Desktop 及其他任何界面。一次托管调用会把同样的计数经由 `llmwire.Usage` 携带到 `llm_call` 账本行上，供 Space 的运行账本路由和 Portal 的运行花费视图读回。

零并不等于未命中。一个不报告缓存计数的提供方，和一个确实未命中的提供方是无法区分的，因此各界面只在提供方确实发来过拆分数据时才展示它，而不会打印一个谁都没有测量过的 `0 / 0`。

## 图像输入

`Config.Vision` 表示该模型接受图像。它之所以存在，是因为一个不支持图像的模型，在收到携带图像的请求时会直接拒绝，而不是忽略它；而图像的产生方——比如返回一张截图的 MCP server——并不知道自己正在和哪个模型对话。

一条消息把图像放在 `Parts` 里，`Content` 则保存描述这些图像的文字。当 `Vision` 为 false 时，适配器只发送文字，这依然是一个完整的工具结果。当它为 true 时，图像的放置位置由协议决定：

| 协议 | 工具图像放在哪里 |
|---|---|
| Anthropic | 放在 `tool_result` 块内部，该协议在这里接受图像 |
| Chat Completions、Responses、Ollama | 紧跟在工具结果之后的一个简短 user 回合，因为它们都不接受在 tool 消息上携带图像内容 |

这个后续回合会带上一行说明性前言。如果没有它，这些图像就会以一个没有任何说明的 user 回合出现，读起来就像是用户自己发来的一样。

## 重试

两种调用方法都会重试至多 `maxRetryAttempts`（3）次，退避时间为 1 秒、2 秒、4 秒：

| 会重试 | 从不重试 |
|---|---|
| 速率限制（429） | Context 取消或截止时间到达——调用方已经放弃 |
| 服务端错误（500、502、503、504） | 鉴权错误（401、403）——需要用户处理 |
| 网络层错误（连接被拒、DNS） | 请求错误（400）——重试无济于事 |
| | 适配器标记为永久性的失败：本地守护进程未运行、模型尚未拉取 |

**流式调用一旦发出过增量内容，就不再重试。** 在用户已经看到部分输出之后再重试，会造成内容重复，因此流式过程中途的失败会以错误的形式呈现出来，而不是发起第二次尝试。

错误会由 `wrapLLMError` 包装上一个人类可读的分类，这也是为什么一个错误的密钥会产生一条能看懂的消息，而不是一个原始的 HTTP 错误。

重试判断和分类都读取自 `apiError`——一种中性的形状，每个适配器都会把自己所用库的失败转换成这种形状。原始错误会被保留并可以被解包，因此确实了解某个具体库的错误类型的调用方，依然能够拿到它。

只有调用方知道被拒绝的密钥该在哪里修复，因此由 `Config.CredentialHint` 为 401 或 403 提供下一步。为空表示本地模型条目，提示为 "check api_key in settings.yaml"；服务器的客户端工厂会给出目录模型及其 `model set-key` 命令，派生的对话目标则给出 `conversation.model.api_key`。

本包之外的调用方通过 `errors.Is` 对照 `core/llm.ErrProviderAuth`（401、403）和 `core/llm.ErrProviderRateLimited`（429）来区分拒绝，`apiError` 按状态码匹配它们。受管网关用它们分类，因此从不读取提供方的原文，也不导入本包。

## 用量采集

Chat Completions 所用的库不会从流式分片中暴露 token 用量，因此 `usageCaptureTransport` 会在响应体流式经过时对其进行检查，并在出现用量块时将其解析出来。这就是为什么流式运行依然能够报告 token 计数。

这个变通方案只局限于这一个适配器。Responses 和 Anthropic 适配器都从各自的事件流中读取用量，不需要类似的处理。

用量由每个适配器归一化为 `core/llm.Usage`。Anthropic 协议不报告总数，因此它的适配器会自行计算——计量读取的是 `TotalTokens`，把它留成零，就等于报告了一次不花钱的调用。`CacheReadTokens` 和 `CacheWriteTokens` 是这份规范形状的一部分，无论阻塞式还是流式，每一个结果都会携带它们。

## 单次调用超时

`CallTimeout` 用 `context.WithTimeout` 包裹每一次单独的尝试——它限定的是一次调用，而不是整次运行。一次包含多轮工具调用迭代的运行，由 Agent 循环中的 `MaxIter` 来限定，而不是在这里。

## 依赖

- **使用**：`internal/core/llm`（契约与消息类型）、`github.com/sashabaranov/go-openai`（两种 OpenAI 协议均使用）、`github.com/anthropics/anthropic-sdk-go`。Ollama 适配器不需要任何客户端库：三个接口和一路以换行分隔的流，不值得为此引入一个模块依赖。
- **使用方**：`internal/agentapp`（客户端缓存）、`internal/bootstrap`（Tier 1 Conversation 客户端）

## 说明

- 任何兼容 OpenAI 的端点，只需更改 `BaseURL` 即可工作——OpenRouter、Azure、本地的 vLLM 或 LM Studio 皆是如此。只有当端点使用不同的协议时，才需要设置 `Provider`。本地 Ollama 守护进程本身也提供这样一个兼容端点，但对它而言正确的取值是 `ollama`：兼容端点无法设置上下文窗口。
- 承重测试是 `conformance_test.go` 中的跨适配器一致性测试套件：同一个逻辑回复会由每种协议的 fixture 编码出来，再经由每个适配器读回，最终得到的规范内容、工具调用和用量必须完全一致。
- 由于 Agent 依赖的是接口而非这个具体结构体，测试可以替换成一个假客户端，完全不接触网络。
- 另见：[Agent 循环](agent-loop.md)、[配置](config.md)。
