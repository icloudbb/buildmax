# Worker API 网络边界

> **翻译说明：** 本文是[英文原文](../../design/worker-api-network-boundary.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **读者：** 贡献者与运营人员 · **状态：** 已交付

相关文档:[Worker run token](Worker运行令牌.md)、[Agent Core trust
harness](信任保障.md) 第 3.9 节、[Agent-scoped sandbox
policy](Agent沙箱策略.md)、[Graceful shutdown](优雅关闭.md),以及
[Enterprise deployment](企业部署.md)。

## 目录

- [1. 状态](#1-状态)
- [2. 问题](#2-问题)
- [3. 决策](#3-决策)
- [4. 安全边界](#4-安全边界)
- [5. Listener 与路由模型](#5-listener-与路由模型)
- [6. 传输认证与加密](#6-传输认证与加密)
- [7. Kubernetes 拓扑](#7-kubernetes-拓扑)
- [8. Run 授权仍然适用](#8-run-授权仍然适用)
- [9. 生命周期与可用性](#9-生命周期与可用性)
- [10. 配置形状](#10-配置形状)
- [11. 已考虑的方案](#11-已考虑的方案)
- [12. 实施计划](#12-实施计划)
- [13. 验证](#13-验证)
- [14. 风险与开放问题](#14-风险与开放问题)
- [15. 文档变更](#15-文档变更)

## 1. 状态

- roadmap_priority：`R0`——验证已交付的 Worker API 边界候选版本
- status:已交付。M1 在同一个进程内用第二个 listener 提供 Worker 控制
  API,拥有自己独立的 mux 和一套失败即关闭的配置;M2 让这个 listener 说
  TLS,而 Worker 则通过一个基于所配置信任关系构建的、显式的 HTTP 客户端
  来访问它,除非明确选择启用,否则会拒绝一个使用 http 的 `k8s_job` URL;
  M3 为参考用的生产环境清单和 kind 清单加上了 `buildmax-api` 与
  `buildmax-worker-api` 两个 Service、Worker 端口、只放行带标签的 Worker
  Pod 的 `NetworkPolicy`、一个只指向 `buildmax-api` 的 Ingress,以及挂载
  进 Worker Job 的 CA 证书;M4 让每一条 Worker 路由都强制检查自己允许的
  TaskRun 状态——先认领 Run 再读取 Secret、只消费被固定住的那个修订版本、
  一旦 Run 进入终态就不再拥有任何能力;M5 让 kind 冒烟测试生成 Worker
  listener 的证书、让 Worker 通过 HTTPS 运行,并在同一次部署中验证这条
  边界——一个带标签的 Worker Pod 能访问 Worker 端口,一个不带标签的 Pod
  会被拒绝,而公共 Service 上的 `/api/worker` 会返回 `404`。更广泛的
  Pod 级 Worker 出站属于 [trust-harness.md](信任保障.md) 第 3.9 节中
  条件触发的 Beta 后加固。
- decision_date:`2026-09-05`
- scope:把 Server 的 Worker 控制通道从它的公共 HTTP 界面中隔离出来,
  并对其传输过程做身份验证

本文回答的是 [trust-harness.md](信任保障.md) 第 3.9 节所述集群网络缺口中
一个范围有限的部分:Worker 如何访问 Server。它并不决定一次 Run 可以访问
哪些 Git 主机、软件包仓库、模型端点,或其他目的地。标准的 Kubernetes
`NetworkPolicy` 无法表达进程内沙箱代理所执行的那种域名策略,所以那个更
广泛的出网决策仍然留在原处未动。

## 2. 问题

目前 Server 只对外暴露一个 HTTP listener。Portal、用户、Webhook 和 Worker
的路由全部注册在它上面。生产环境的 Ingress 会把整个 `/api` 前缀都转发给
这个 listener,因此尽管本应只有 Worker Job 才会调用 `/api/worker/*`,
这些路由却仍然可以从公网访问到。

Kubernetes 上的 Worker 通过明文 HTTP 使用同一个 Service 和同一个 listener:

```text
Internet / Portal
       |
       | HTTPS
       v
Ingress ----------------------+
                              v
Worker Pod -- plain HTTP --> buildmax Service :5678 --> one route set
                                                        |- user API
                                                        |- Portal API
                                                        `- worker API
```

运行令牌仍然会对每一个 Worker 请求做身份验证,这能防止未经身份验证的
调用方使用 Worker 路由。但它无法提供传输层的机密性,也无法验证 Server
自身的身份。在这套参考拓扑中,一个 Bearer Token、Task 的输入、流式输出,
以及一次 Space Secret 响应,全都会以明文的形式穿越 Pod 网络。

共享同一个 listener 同样使得 Kubernetes 网络策略无法表达出预期的边界。
一个 Service 和一个 `NetworkPolicy` 匹配的是地址和端口,而不是 HTTP 路径。
当所有路由共用同一个端口时,它们没有办法一边允许 Worker 访问
`/api/worker/*`,一边又拒绝同一个来源访问 `/api` 下的其他内容。

所以,当前的形态存在四个原本可以避免的问题:

| 问题 | 后果 |
|---|---|
| Worker 路由与公共 listener 共用 | 公共 Ingress 会把它们一并暴露出去 |
| Worker 流量使用 HTTP | 网络监听或流量劫持可能泄露一个 Bearer Token 及响应数据 |
| 所有 API 共用一个端口 | 三层/四层网络策略无法区分出 Worker 流量 |
| 运行令牌是唯一的调用方边界 | 一个泄露的令牌可以从能够访问该 listener 的任意网络位置被重放 |

## 3. 决策

Server 将会暴露两个各自独立路由的 HTTP listener:

| Listener | 默认地址 | 路由 | 预期调用方 |
|---|---|---|---|
| 公共 | 现有的已配置端口,通常是 `:5678` | 除 Worker 之外的所有路由,包括 Portal、用户 API、Webhook、WebSocket、健康检查、OpenAPI 和 Swagger | Ingress、运营人员、CLI、Desktop |
| Worker | 除非明确配置,否则为 `127.0.0.1:5679` | 仅 `/api/worker/*` | 同一台主机上的 `local_process` Worker,或者通过内部 Service 访问的 Worker Pod |

它们各自对应的 Kubernetes Service 规范名称分别是 `buildmax-api` 和
`buildmax-worker-api`。日志和内部代码中使用的 listener 名字是 `api` 和
`worker-api`;`public`(公共)描述的是可达性,并不是资源名的一部分,
因为一个位于 Ingress 之后的 `ClusterIP` 本身并不是一种公共 Service 类型。

安全的默认值会把 Worker listener 绑定到回环地址。一个 Kubernetes 部署
必须刻意把它绑定到 `:5679`、配置 TLS,并创建对应的内部 Service。因此,
一次意外发生的默认部署不会因此暴露出一个新的、未经身份验证的集群端口。

生产环境的拓扑结构变为:

```text
Internet / Portal
       |
       | HTTPS
       v
Ingress --> buildmax-api Service :5678 --> public listener

Worker Pod
       |
       | HTTPS + run token
       v
buildmax-worker-api ClusterIP :5679 ------> worker listener
```

这两个 listener 最初仍然位于同一个 `buildmax-server` 进程中,共用同一套
存储和服务。这建立的是一条网络与路由层面的边界,而不是进程隔离边界。
未来如果需要把公共处理器的失陷,与调度器及 Worker 权限隔离开来,可以把
Worker listener 和调度器迁移到另一个二进制文件或另一个 Deployment 中,
而不需要改变本文所确定的协议。

## 4. 安全边界

### 4.1 本决策解决的威胁

- 一个未经身份验证的互联网调用方,无法通过公共 listener 访问到一个
  Worker 处理器,即便 Ingress 规则转发了宽泛的 `/api` 前缀也不行。
- 当生产环境的 `NetworkPolicy` 没有把某个普通集群 Pod 选中为 Worker 时,
  这个 Pod 无法连接到 Worker listener。
- 一个在 Worker 网络之外被复制出去的运行令牌,单凭自身不足以访问到
  Worker listener。
- Server 的身份会在一个 Worker 发送它的 Bearer Token、或接收 Task 与
  Secret 数据之前先得到验证。
- Worker 控制流量在集群内部是加密传输的。

### 4.2 本决策未解决的威胁

- 一个合法的 Worker 仍然可以读取它所在的那次 Run 被授权接收的 Space
  Secret 和 Task 数据。
- 一个被攻陷的 Server 进程同时拥有两个 listener 和调度器。
- Kubernetes 管理员、节点管理员、CNI 管理员,或持有 Server 的
  ServiceAccount 的人,仍然是被信任的。
- 一个 Worker Pod 的一般性出网访问,仍然取决于集群本身和 Agent 级沙箱
  策略所允许的范围。本设计限制的只是*到 Worker listener 的访问*;它并不
  声称提供了一条域名感知的出网边界。
- Job 规格中的运行令牌存储方式、令牌的有效期、对象存储凭证,以及以
  root 加 `SYS_ADMIN` 权限运行的 Worker 容器约束,都是各自独立的安全
  欠账,不在本文解决范围内。

### 4.3 边界的构成

没有任何一项控制取代另一项:

| 控制措施 | 建立的效果 |
|---|---|
| 独立的 listener 和路由集合 | 公共套接字无法转发到一条 Worker 路由 |
| 内部 `ClusterIP` Service | 不会造成刻意的外部 Kubernetes 服务暴露 |
| `NetworkPolicy` | 只有被选中的 Worker Pod 才能连接到 Worker 端口 |
| TLS | 在传输过程中验证 Server 身份并保证机密性 |
| 运行令牌 | 针对用户、Space、Task 和 TaskRun 的应用层权限 |
| TaskRun 状态检查 | 这份权限当下是否真的可以被行使 |

## 5. Listener 与路由模型

### 5.1 路由注册

路由的唯一事实来源仍然是各个处理器子包各自的 `Register` 方法。组合方式
从一个根 mux 变成了两个:

- 公共 mux 会注册除 Worker 处理器之外的每一条现有路由;
- Worker mux 只注册 `internal/server/handlers/worker`;
- 请求 ID、有界日志、恢复(recovery)以及 HTTP 超时等通用传输层中间件
  同时包裹这两个 mux;
- 面向浏览器的 CORS 中间件只包裹公共 listener;
- 用户访问令牌中间件不会作为兜底逻辑出现在 Worker listener 上。

一条未知的路由在任一 listener 上都会返回 `404`。具体来说:

- `/api/worker/task-runs/...` 在公共 listener 上会返回 `404`,即便请求
  携带了一个有效的运行令牌;
- `/api/spaces/...`、`/api/auth/login`、`/api/webhook`、`/swagger` 和
  `/openapi.json` 在 Worker listener 上会返回 `404`。

公共 OpenAPI 文档可以继续为贡献者完整描述整个协议,但提供这份文档并不
意味着把 Worker 路由注册到了公共 mux 上。这份精确路由的架构测试,必须
把两组路由的并集与文档做比对,并单独校验每条路由所处的 listener。

### 5.2 单靠 Service 本身构不成边界

如果新建一个指向现有 `:5678` 端口的第二个 Kubernetes Service,那只是
给同一个套接字多起了一个名字。它无法阻止公共 Service、Ingress、直接的
Pod-IP 请求,或另一个集群 Pod 访问到 Worker 处理器。

独立的端口和独立的 mux 是必需的。正是 Service 与 `NetworkPolicy` 的
组合,才让这条应用层边界可以被集群真正强制执行。

### 5.3 不存在由 Server 发起的 Pod 连接

这项变更保留了当前的通信方向。Server 通过 Kubernetes API 创建一个
Job;它从不主动向 Pod 打开一条应用连接。而是由 Worker 通过内部
listener,主动发起元数据读取、认领与终态更新、取消轮询、流式传输、
Artifact 发布、插件下载、Secret 物化,以及托管推理调用。

## 6. 传输认证与加密

### 6.1 必需的 Server 身份验证

生产环境的 Worker listener 使用 TLS。它的证书必须包含内部 Service 的
DNS 名称,通常是 `buildmax-worker-api.buildmax.svc.cluster.local`。
Worker 会验证这个名称以及一个已配置的 CA;`InsecureSkipVerify` 不是一种
受支持的生产模式。

第一版实现支持:

- 当内部证书能够链接到系统信任根时,使用系统信任根;以及
- 一份以只读方式挂载进每个 Worker Pod 的显式 CA 文件。

Server 的证书和私钥只会挂载进 Server 的 Pod 中。CA 证书是公开材料,
可以通过 ConfigMap 分发。证书轮换在第一版实现中要靠 Server 重启才能
生效;第一版不要求支持热加载,但这一点必须写入运营文档。

### 6.2 开发环境中的明文 HTTP

明文 HTTP 只有在显式设置了 `allow_insecure_http` 的情况下,才可用于
`local_process`、Compose 和本地 kind 开发环境。一个 `k8s_job` 配置如果
使用了 `http://` 形式的 Server URL,除非同时开启了这个设置,否则会验证
失败。生产环境的参考配置永远不会开启它。

这是一项显式声明,而不是从主机名推断出来的:`.cluster.local`、回环地址
和私有地址描述的都是路由上的事实,并不能证明一个网络就是机密的。

### 6.3 双向 TLS

运行令牌仍然是强制性的客户端身份验证机制。原生 mTLS 在第一版设计中是
一种可选的加固手段,因为一个共享的客户端证书会给每一个 Worker 引入
另一份部署级别的凭证,而按 Pod 逐一签发证书,又需要当前的 Job 尚不具备
的工作负载身份能力。

如果一个服务网格、SPIFFE 签发者,或平台自身的工作负载身份体系已经在为
每个 Pod 签发身份,那么内部 listener 可以在运行令牌之外,额外要求提供
它们的客户端证书。BuildMax 不会把一个 mTLS 身份当作 TaskRun 权限的
凭据:它识别的是工作负载的类别,而运行令牌识别的才是具体的那次 Run。

## 7. Kubernetes 拓扑

### 7.1 Service 与 Ingress

生产环境清单定义了:

- `buildmax-api`,选中 Server Pod 并指向公共端口;
- `buildmax-worker-api`,一个同样选中这些 Server Pod、但指向 Worker
  端口的 `ClusterIP`;以及
- 一个后端只包含 `buildmax-api` 的 Ingress。

这条宽泛的公共 `/api` Ingress 规则可以继续保留,因为公共 mux 本来就不
包含 Worker 路由。运营方特有的反向代理路径规则,作为纵深防御是有用的,
但不是权威的隔离手段。

Worker 的 Job 会拿到
`https://buildmax-worker-api.buildmax.svc.cluster.local:5679` 作为它的
Server URL。Server 证书的私钥永远不会被 Worker 继承。

### 7.2 稳定标签

每一个动态创建出来的 Worker Job 和 Pod,都携带着由 Kubernetes 运行器
所拥有的稳定标签,包括:

```yaml
app.kubernetes.io/name: buildmax-worker
app.kubernetes.io/component: worker
```

TaskRun ID 可以放在一个注解中用于关联,但它不是一个安全层面的选择器,
也绝不能包含任何凭证材料。一个 Worker 没有 ServiceAccount 令牌,因此
无法通过 Kubernetes API 给自己重新打标签。

### 7.3 网络策略

生产环境的 `NetworkPolicy` 选中 Server Pod,并表达出两条入站规则:

- 公共端口仍然可以通过部署正常的公共路径访问到;
- Worker 端口只能被所选执行命名空间中、带有 Worker 标签的 Pod 访问到。

在这套可移植的参考配置中,允许所有来源访问公共端口是可以接受的,因为
公共 API 仍然在应用层做身份验证,而且暴露给 Ingress 本来就是有意为之。
运营方应当在了解自己的 CNI 和健康探针行为的前提下,把这个范围收窄到
自己的 Ingress 控制器所在的命名空间。

这项策略同时也保护了直接的 Pod-IP 访问,而不只是 Service 访问。一个
没有配套这项策略的 `ClusterIP` Service,提供的只是可发现性,而不是
授权。

本设计不会加入一条默认拒绝 Worker 出网的策略。要在保留 Git、软件包
仓库、模型和对象存储访问能力的同时做到这一点，需要
[trust-harness.md](信任保障.md) 第 3.9 节中的条件触发加固决策。
只有部署证据重新开启该路径后，参考配置才增加收窄的 Worker 出网规则。

### 7.4 命名空间边界

第一版实现让 Server 和 Worker 的 Job 都留在同一个已配置的命名空间中。
一个专用的执行命名空间与本设计是兼容的,也能改善隔离效果,但它会改变
调度器的 RBAC、Secret 与 ConfigMap 的分发方式、网络选择器,以及对象
存储的身份认证方式。这是一项后续工作,而不是一个被隐藏起来的前置
条件。

## 8. Run 授权仍然适用

把一个处理器移到内部 listener 上,并不会让它因此就变得可信。每一条
Worker 路由仍然需要一个运行令牌,需要把令牌里的 `rid` 声明与路径相
匹配,并且必须从 Server 状态和已签名的声明中推导出 Space 和用户的归属
信息,而不是从请求体中获取。

这次 listener 层面的改动,绝不能把当前生命周期方面的缺口当作预期行为
固化下来。以下这些生命周期授权,现在已经在 Worker 路由自身上得到强制
执行(M4),叠加在运行令牌之上:

- Secret 的物化要求这次 Run 处于 RUNNING 状态——也就是已被认领的、
  活跃的状态——而且 Worker 会在获取 Secret 值之前先认领这次 Run;
- 物化过程读取的是被固定在这次 TaskRun 上的 Agent 修订版本所对应的
  消费配置,而不是该 Agent 当前的修订版本,因此在运行过程中编辑配置,
  无法扩大一次正在进行的 Run 所能接收到的内容;
- 一次已处于终态的 Run 无法再进行流式传输、发布 Artifact、读取
  Secret、添加 Issue 评论、下载插件,或发起一次托管模型调用——一个
  泄露了但尚未过期的运行令牌,在这次 Run 结束之后同样会被拒绝;而
- `getTaskRun` 仍然是唯一的例外,因为一个重启后的 Worker 必须能够
  在任意状态下读取一次 Run,才能发现它其实已经处于终态。

一项路由 × 状态矩阵测试,枚举了每一条 Worker 路由及其允许的 TaskRun
状态,其中也包括"完成之后使用一个泄露但尚未过期的令牌"这种情形。这是
叠加在网络边界之上的应用层授权,与
[Space Secrets and run delivery](Space密钥.md) 第 7 节的规定一致。

## 9. 生命周期与可用性

### 9.1 启动

Server 会在打开任何一个 listener 之前,先把两组路由都构建完成。当
Worker 执行功能被启用时,如果 Worker listener 绑定失败或配置有误,
会导致 Server 启动失败;"接受用户的 Task,却没有任何 Worker 能够报告
它们的执行情况",不能算作一种可接受的降级模式。

Worker listener 的 TLS 配置会在调度器启动之前完成校验。Server 绝不能
带着一个 HTTP URL 就去调度 Job,直到 Pod 内部才发现配置有误。

### 9.2 关闭

优雅关闭会保留一个正在运行的 Worker 上报状态的能力:

1. 把公共就绪状态标记为 false,停止调度新的 Run;
2. 让公共请求和对话轮次静默下来并排空;
3. 在现有关闭预算中,为 Worker 上报状态这部分保留 Worker listener 的
   开放时间;
4. 排空 Worker listener;以及
5. 关闭各个存储并退出。

因此,Worker listener 会在公共 listener 之后关闭,而不是与它同时关闭。
延长它的排空窗口,必须仍然落在 Pod 的 `terminationGracePeriodSeconds`
约定之内,参见 [graceful-shutdown.md](优雅关闭.md)。

### 9.3 多副本

这个内部 Service 可能会把来自同一个 Worker 的连续请求,负载均衡到不同
的 Server 副本上。运行令牌的验证,以及持久化的 TaskRun 状态转换,本来
就已经使用共享状态,必须与具体哪个副本处理无关。本文不会修复
`ROADMAP.md` R1 所跟踪的内存态 WebSocket 和对话轮次队列的局限性。

## 10. 配置形状

预期的 `server.yaml` 形状是:

```yaml
port: 5678

worker_api:
  listen: "127.0.0.1:5679"
  tls:
    cert_file: ""
    key_file: ""
    client_ca_file: ""       # optional native mTLS

worker:
  run_mode: k8s_job
  server_url: https://buildmax-worker-api.buildmax.svc.cluster.local:5679
  allow_insecure_http: false
  server_ca_file: /buildmax/tls/worker-api-ca.crt
  client_cert_file: ""       # optional native mTLS
  client_key_file: ""
```

准确的字段名称,会在同一次改动中一并进入
`internal/config/server_config.go`、`config-examples/server.example.yaml`,
以及[Configuration reference](../reference/configuration.md)。不会为此
新增环境变量形式的替代配置:listener 和信任相关的配置属于结构化的部署
策略,而不是引导阶段的密钥。

校验规则:

| 情形 | 结果 |
|---|---|
| `worker_api.listen` 与公共 listener 相同 | 拒绝启动 |
| TLS 证书和密钥恰好只设置了其中一个 | 拒绝启动 |
| Worker URL 使用 HTTPS,但不存在可用的信任根 | 拒绝启动 |
| `k8s_job` 使用 HTTP,却没有开启 `allow_insecure_http` | 拒绝启动 |
| mTLS 客户端证书和密钥不完整 | 拒绝启动 |
| Worker URL 指向了公共 listener | 只要两个地址都能从配置中解析出来,就拒绝启动 |

这些路径都是配置值,Server 和 Worker 的挂载位置可以不同。生产环境清单
只把 Server 的私钥挂载进 Server Pod,而把 CA 挂载进 Worker。

## 11. 已考虑的方案

### 11.1 在 Ingress 层拒绝 `/api/worker`

被否决,不能作为边界。Ingress 的行为取决于具体的控制器实现,直接的
Service 和 Pod-IP 访问可以绕过它,而且公共 listener 里仍然会包含这些
处理器。它仍然是一种有用的纵深防御手段。

### 11.2 在现有端口上新增第二个 Service

被否决。两个 Service 会指向同一个套接字和同一组路由,Kubernetes 因此
没有任何可以强制执行的授权边界。

### 11.3 单一 listener,在 Worker 路径上使用 mTLS

被否决。Go 的 TLS 客户端身份验证发生在 HTTP 路径已知之前;要求某一条
路径提供客户端证书、另一条不要求,就需要另一个 TLS 终止层或 listener。
而且这样一来,Worker 处理器仍然会注册在公共 mux 上。

### 11.4 同一进程中的两个 listener

被采纳。它用较小的运维改动,建立起真实的端口与路由边界,保留了单一
调度器和服务拓扑,而且未来可以在不改变 Worker 协议的前提下,拆分成
另一个进程。

### 11.5 立即拆分出一个独立的 Worker 控制部署

被推迟。它能提供最强的进程边界,但在部署证据表明这份代价确有必要之前,
就要先重复一遍引导、健康检查、发布和数据库连接的工作。两个 listener
的设计让这种未来的迁移依然是可行的。

## 12. 实施计划

### M1. 路由与 listener 分离——已交付

- 构建独立的公共 mux 和 Worker mux。
- 用现有的超时和日志策略,运行两个相互协调的 `http.Server` 实例。
- 只在内部 mux 上注册 Worker 路由。
- 加入配置解析,以及失败即关闭的校验逻辑。
- 保留原有的、针对并集的精确 OpenAPI 路由测试,并新增一项位置断言。

验收标准:一个有效的运行令牌无法通过公共 listener 访问到一个 Worker
处理器,一个有效的用户令牌也无法通过 Worker listener 访问到一个公共
处理器。

### M2. TLS 与 Worker 客户端信任——已交付(把 CA 挂载进 Job 是 M3 的内容)

- 为 Worker listener 加入原生 TLS。
- 给 `workerclient` 一个基于所配置信任根构建的、显式且可复用的 HTTP
  客户端,而不是使用 `http.DefaultClient`。
- 把私有 CA 证书挂载进 Worker 的 Job 中。
- 默认拒绝不安全的 Kubernetes Worker URL。

验收标准:Worker 能够通过 HTTPS 完成一次 Run,会拒绝错误的服务器名称
和错误的 CA,并且永远不会回退到 HTTP。

### M3. Kubernetes 边界——已在参考清单中交付(kind 上的 HTTPS 是 M5 的内容)

- 在生产环境和 kind 清单中加入内部 Service 和 Worker 端口。
- 让 Ingress 只指向 `buildmax-api` 这个 Service。
- 给生成出来的 Job 和 Pod 一致地打上标签。
- 为 Worker 端口加入 Server 入站方向的 `NetworkPolicy`。
- 继续禁用 Worker 的 ServiceAccount 令牌自动挂载。

验收标准:一个带标签的 Worker Pod 能够访问到 Worker listener;同一
命名空间中一个不带标签的 Pod 做不到;二者都无法通过 `buildmax-api`
这个 Service 访问到一个 Worker 处理器。

### M4. 路由生命周期授权——已交付

- 在释放 Space Secret 材料之前,先认领这次 Run。
- 为每一条 Worker 路由强制执行一份状态矩阵。
- 从被固定的那个 Agent 修订版本中解析 Secret 消费配置。
- 除了这次 Run 早已提交、本来就具备幂等性的终态报告之外,拒绝一切
  在终态之后发起的 Worker 能力调用。

验收标准:这份路由表测试同时证明了凭证范围和生命周期范围,包括"一个
泄露了但尚未过期的令牌,在完成之后被使用"这种情形。

### M5. 部署证据——已交付

- 更新 Compose 配置,让开发环境使用明文 HTTP 变成一项显式选择。
- 更新 kind 配置,以练习 HTTPS 路径和 `NetworkPolicy` 拒绝的场景。
- 更新生产环境参考配置及其配置解析测试。
- 让常规的 Worker TaskRun 冒烟测试和托管推理冒烟测试,都经由这个内部
  Service 运行。

验收标准:那次成功的冒烟测试,和那次被拒绝的跨 Pod 探测,是同一次
部署产生的两份证据,而不是分别手工搭建出来的复现。

## 13. 验证

### 13.1 确定性测试

- 公共 mux 中不包含任何以 `/api/worker/` 开头的路由模式。
- Worker mux 中只包含 Worker 路由模式。
- 每一条 Worker 路由都会拒绝:没有令牌、一个用户令牌、另一次 Run 的
  令牌、一个已过期的令牌,以及一个由另一个部署签发的令牌。
- 每一条 Worker 路由都强制执行它所允许的 TaskRun 状态。
- listener 的配置会拒绝端口冲突,以及不完整的 TLS 材料。
- 当启用了 mTLS 时,Worker 的 TLS 会拒绝错误的主机名、错误的 CA、
  已过期的证书,以及缺失的客户端证书。
- 这个 Kubernetes Job 携带了 Worker 标签、内部 URL 和 CA 挂载,但不
  携带 Server 的私钥,也不携带数据库/JWT 凭证。

### 13.2 部署测试

kind 冒烟测试必须在同一个已安装的拓扑中证明以下全部内容:

1. 公共 API 和 Portal 流量仍然经由 `buildmax-api` 这个 Service 进入;
2. `buildmax-api` 这个 Service 上的 `/api/worker/*` 返回 `404`;
3. 一个带标签的 Worker 能够通过 HTTPS 完成一次 TaskRun;
4. 一个不带标签的探测 Pod 无法连接到 Worker 端口;
5. Worker 会拒绝一个对该 Service DNS 名称无效的证书;
6. 取消、心跳、流式传输、Artifact、Secret、插件,以及托管 LLM 相关的
   路径,仍然能够通过这个内部 listener 正常工作;并且
7. 优雅关闭为一次正在进行中的 Worker 终态报告留出了足够的时间。

生产环境清单的解析检查,必须断言这两个端口、HTTPS URL、TLS 挂载、
Service 类型、Ingress 后端、标签以及策略选择器。一份仅仅能被解析的
YAML 文件,并不能证明这条边界真的被接通了。

## 14. 风险与开放问题

| 问题 | 初步答案 |
|---|---|
| 第二个 listener 是否意味着需要第二个二进制文件? | 不需要。先从一个进程、两个 mux 开始;只有在有部署证据支持时才拆分 |
| 没有 `NetworkPolicy` 的 `ClusterIP` 是否足够? | 不够。它能防止刻意的外部服务暴露,但无法阻止直接的集群内访问 |
| 没有 mTLS 的 TLS 是否足够? | 它能保护令牌并验证 Server 身份;`NetworkPolicy` 加上运行令牌共同完成对调用方的身份验证。在存在工作负载身份的场景下,优先采用逐 Pod 的 mTLS |
| 公共 OpenAPI 是否应该描述 Worker 路由? | 目前应该继续描述,前提是注册相关的测试能证明"被描述"不等于"可访问" |
| Worker 出网在本次改动中能否变成默认拒绝? | 不能。Pod 级目的地强制属于 `trust-harness.md` 第 3.9 节中条件触发的 Beta 后加固 |
| Server 和 Worker 是否应该迁移到不同的命名空间? | 是一项兼容的后续工作;不是建立这条 listener 边界的必要条件 |
| 证书如何签发和轮换? | 由运营方/平台提供;先采用基于重启的轮换方式,只有在运营证据确有需要时才引入热加载 |

这项实施中最大的风险,是给同一个套接字起了两个名字,却把这称为隔离。
正因如此,验收测试都是围绕路由和端口来构建的。

## 15. 文档变更

当本设计交付上线时:

- [Configuration reference](../reference/configuration.md) 记录这两个
  listener 及其 TLS 相关字段;
- [Production deployment](../../../deployment/production/README.md)
  解释证书、Service 和策略方面的约定;
- [Server architecture](../contribute/architecture/server.md) 记录这两个
  mux 以及关闭顺序;
- [Worker run token](Worker运行令牌.md) 不再把网络可达性描述得好像
  令牌范围就是整条边界;
- [Trust harness](信任保障.md) 把第 3.9 节中关于 Server 控制通道的
  那部分标记为已关闭，同时把一般性 Worker 出网推迟到有证据时再开启；
  并且
- [Support matrix](../../../manual/support.md) 只有在 kind 的拒绝探测
  成为常规冒烟测试的一部分之后,才去描述这条已部署的边界。
