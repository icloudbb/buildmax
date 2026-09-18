# Harbor 运行流程备忘录

> **Audience:** evaluation maintainers · **Status:** current for the Harbor version pinned in [`pins.json`](pins.json)

本文记录 BuildMax 通过 Harbor 运行外部 benchmark 时，从命令入口到最终
BuildMax trial bundle 的完整控制流。它是实现导读，不替代
[`../../docs/design/evaluation-system.md`](../../docs/design/evaluation-system.md)
中的评测系统决策，也不描述未验证的上游最新行为。

仓库当前由 [`pins.json`](pins.json) 固定 Harbor、dataset 和 adapter 版本；切换
Terminal-Bench 数据集版本时，必须重新验证本文涉及的 task 格式、环境能力、结果
格式和失败分类，不能只替换 dataset 名称。

## 1. 一句话模型

Harbor 的运行主线是：

```text
harbor run
   │
   ├─ 解析并冻结 Job 配置
   ├─ 下载或解析 Dataset 与 Task
   ├─ 展开 Task × Agent × Attempt
   │
   └─ 并发运行 Trial
          │
          ├─ 创建隔离环境
          ├─ 安装 Agent
          ├─ 给 Agent 指令并等待执行
          ├─ 收集日志、轨迹和产物
          ├─ 运行 Task 自己的 verifier
          ├─ 读取 reward
          └─ 销毁环境并写入 result.json
   │
   └─ 聚合 Job 结果、指标、成本和失败信息
```

Harbor 不只是一个容器启动器。它同时解释 Task 包、控制 Trial 生命周期、调用
Agent adapter、运行 verifier、实施重试和超时，并生成可追溯的结果目录。

## 2. 核心对象及所有权

| 对象 | 含义 | 主要所有者 |
|---|---|---|
| Job | 一次批量评测，包含多个 Trial | Harbor |
| Trial | 一个 Agent 对一个 Task 的一次独立尝试 | Harbor |
| Task | instruction、环境、测试、资源和超时的版本化定义 | benchmark 作者 |
| Environment | Docker、云沙箱或 Kubernetes 等执行后端 | Harbor environment adapter |
| Agent adapter | 安装和调用某种 Agent，并回收其用量及轨迹 | BuildMax 的 Python adapter |
| Verifier | 根据最终环境状态产生 reward | Task 作者；Harbor 负责执行 |
| Trial bundle | BuildMax 内部统一的评测证据格式 | BuildMax importer |

最重要的所有权边界是：Harbor 的 verifier 给出外部 benchmark 的权威 verdict；
BuildMax importer 只转换和补充证据，不重新评分。

## 3. BuildMax 命令入口

典型入口是：

```shell
./make eval harbor run \
  --task terminal-bench/pypi-server \
  --model anthropic/claude-opus-4-7
```

进入 Harbor 之前，BuildMax 的 task runner 和评测工具完成以下工作：

1. 检查 Harbor、执行后端和待测二进制等前置条件；模型访问由 adapter 在创建
   Trial 环境前单独做 fail-fast 校验。
2. 在未传 `--binary` 时构建 Linux/amd64 BuildMax CLI。
3. 从 [`pins.json`](pins.json) 读取不可浮动的 dataset ref、Harbor 版本和
   adapter import path。
4. 验证任务选择。调用者必须显式传 `--task`、`--canary`、`--limit` 或
   `--all`，避免无意运行整个付费数据集。
5. 生成并执行真正的 `harbor run` 命令。

命令大致如下：

```shell
harbor run \
  -d terminal-bench/terminal-bench-2-1@sha256:<pinned-ref> \
  -a buildmax_harbor.agent:Buildmax \
  -m anthropic/claude-opus-4-7 \
  --include-task-name terminal-bench/pypi-server \
  -k 1 \
  -o .artifacts/harbor/jobs \
  --ak binary=bin/buildmax-linux-amd64
```

[`run.go`](run.go) 负责从 pins 和调用参数构造命令；
[`../../tools/eval/harbor_run.go`](../../tools/eval/harbor_run.go) 负责选择任务、
启动运行以及在结束后触发导入。

## 4. Job 解析和 Trial 展开

Harbor 收到命令后，先构造逻辑 Job：

1. 根据 dataset 名称和 ref 解析 manifest。
2. 下载或命中本地缓存中的 Task 包。
3. 解析每个 Task 的 instruction、配置、environment、tests 和可选 solution。
4. 将已解析输入写入 Job 配置及 lock evidence。
5. 按 attempt、Task 和 Agent 配置展开 Trial。

Trial 数量近似为：

```text
Task 数量 × Agent 配置数量 × attempts
```

`attempt` 和 `retry` 必须分开理解：

- attempt 是进入统计的独立样本，例如 `-k 5` 表示每道题五次独立尝试；
- retry 是同一个逻辑 attempt 因 API、环境或 harness 异常而重新执行；
- verifier 正常给出 `reward=0` 表示 Agent 没完成任务，是有效测量，不应重试。

Harbor 的 Trial queue 使用并发信号量限制总并发，也可以限制某个 Agent 或模型
连接的并发。每次 retry 会重新创建 Trial 和环境，并按配置执行退避。

## 5. Trial 初始化

一个 Trial 开始时，Harbor 会创建独立目录，并写入：

```text
<trial-name>/
├── config.json
├── lock.json
├── agent/
├── verifier/
└── artifacts/
```

随后它会：

1. 加载 Task，记录 task checksum。
2. 解析 Agent 配置并实例化 BuildMax adapter。
3. 根据 Task 和命令行选择 Environment 实现。
4. 解析 Agent 和 verifier timeout。
5. 校验资源、网络策略、Compose、sidecar artifact 等能力是否被后端支持。
6. 决定 verifier 使用共享环境还是独立环境。

这些检查应尽量发生在拉取镜像和调用模型之前，因为此后的失败已经会消耗时间或
费用。

## 6. 环境物化

### 6.1 单容器 Task

Task 只有 Dockerfile 或预构建镜像时，Harbor 的 Docker environment 创建一个
主要服务：

```text
Trial
└── main
    └── /app    # Agent 工作目录
```

Docker environment 会生成运行时 Compose overlay，处理镜像构建或拉取、资源
限制、环境变量、网络策略和日志挂载，然后启动环境并等待 healthcheck。

### 6.2 多容器 Task

Task 带有 `environment/docker-compose.yaml` 时，题目可以声明数据库、消息队列、
客户服务或负载发生器等 sidecar：

```text
Trial
├── main       # Agent 在这里运行
├── postgres
├── redis
├── kafka
└── loadgen
```

Harbor 启动和销毁整个 Compose project。Agent 仍只在 `main` 中工作，通过服务名
和普通网络协议访问 sidecar；它不需要 Docker socket，也不负责创建容器。

Agent 结束后，Harbor 可以在 sidecar 中执行 collect hook，将数据库快照、请求
日志或内存计数导出成 artifact，再交给 verifier。这个能力是“环境提供服务”和
“Agent 管理基础设施”之间的边界。

## 7. BuildMax adapter 安装阶段

环境通过 healthcheck 后，Harbor 调用 Agent adapter 的 `setup()`。当前实现位于
[`src/buildmax_harbor/agent.py`](src/buildmax_harbor/agent.py)。它会：

1. 安装 BuildMax 运行需要的最小系统依赖。
2. 从启动 Harbor 的主机上传指定 BuildMax 二进制。
3. 将其安装到 `/usr/local/bin/buildmax`。
4. 在上传前计算 artifact digest。
5. 在容器内执行 `buildmax --version`，记录实际运行的版本和 commit。

因此 subject identity 描述的是实际进入 Trial 的二进制，而不是事后推测的工作树
revision 或 `--binary` 路径字符串。

## 8. Agent 执行阶段

Harbor 调用 adapter 的：

```python
agent.run(instruction, environment, context)
```

BuildMax adapter 在容器内创建一次性的：

```text
/tmp/buildmax-home/
└── settings.yaml
```

这个 home 只包含本次 Trial 显式解析出的模型连接、reasoning、context window、
输出上限和可选价格，不继承维护者自己的 `~/.buildmax/settings.yaml`。

随后 adapter 在 Harbor 已准备好的工作目录中执行：

```shell
BUILDMAX_HOME=/tmp/buildmax-home \
HOME=/tmp/buildmax-home \
/usr/local/bin/buildmax \
  -p "<task instruction>" \
  --output json \
  --no-stream \
  </dev/null \
  >/logs/agent/buildmax-result.json
```

它故意不传 `--workspace`。BuildMax 使用当前目录，也就是 Task 提供的工作环境；
Agent 看见的是题目文件和依赖服务，而不是 BuildMax 仓库。

模型 API key 当前会被写入 Agent 容器内的临时 `settings.yaml`。运行结束时删除 home
可以缩短凭证留存时间，但不构成对 Agent 所执行代码的凭证隔离。企业级执行后端若
要求更强边界，应使用短期、限权的凭证或模型网关，而不能把“结束后删除文件”当作
秘密不可见的证明。

## 9. Timeout、退出码和 Agent 证据

Harbor 在 Agent 调用外层实施 timeout。取消等待不总能终止容器中的子进程，因此
BuildMax adapter 在 `finally` 中额外执行：

```shell
pkill -x buildmax || true
```

然后它尽力完成：

1. 将 session 和 trace 复制到 `/logs/agent/sessions/`。
2. 保留 `/logs/agent/buildmax-result.json`。
3. 删除含凭证的 `/tmp/buildmax-home`。

BuildMax 的 iteration-cap 退出码是一个特例。Adapter 将其吞掉，让 Task verifier
继续判断当前工作区，因为“预算已耗尽”不等于“没有可评判结果”。其他非零退出通常
作为 Agent 异常传播给 Harbor，并可能触发 retry。

Agent 阶段结束后，Harbor 将 adapter 上报的 token、cache、cost 和 metadata 写入
Trial result。BuildMax importer 后续会优先使用原始 `buildmax-result.json`，因为
它比 Harbor 的通用字段保留更多精度和 BuildMax 特有信息。

## 10. Artifact 收集

Agent 完成后，Harbor 收集两类证据：

- Agent 自己写到约定日志目录的 result、trajectory 和 session；
- Task 显式声明的 artifact，包括来自 `main` 或 sidecar 服务的文件。

多容器 Task 还可以先执行 collect hook，把数据库或进程内状态固化到文件，再下载
到 Trial 目录。Artifact manifest 记录来源服务和路径，使 verifier 和结果读者能够
区分证据来自 Agent 容器还是依赖服务。

## 11. Verifier 阶段

Verifier 在 Agent 阶段之后运行，最终 reward 由 Task 自己的测试决定。

### 11.1 共享环境

共享模式在 Agent 使用过的环境中上传 tests，再执行 Task 的测试脚本：

```text
main
├── Agent 修改后的工作目录
└── /tests/test.sh
```

测试脚本将结果写入：

```text
/logs/verifier/reward.txt
```

或：

```text
/logs/verifier/reward.json
```

Terminal-Bench 2.1 的当前导入约定读取单一 `reward`，并将 `1` 视为通过、低于 `1`
视为未通过。Reward 文件才是 verdict；测试脚本进程退出本身不能替代它。

### 11.2 独立 verifier 环境

Harbor 也支持让 verifier 在独立环境中运行：

```text
Agent 环境
   │
   ├─ 收集允许导出的 artifacts
   └─ 停止或销毁
          │
          ▼
独立 verifier 环境
   ├─ 隐藏 tests
   ├─ grader 依赖
   ├─ 显式 artifacts
   └─ 运行 test.sh
```

这种模式适用于隐藏测试、grader 专用依赖或 clean-room 判定。代价是 verifier 不再
自然拥有 Agent 的整个可变工作区；Task 必须明确设计需要传递的 artifact 或状态。
共享还是独立由 Task 配置和 Harbor 解析决定，不由 BuildMax adapter 或 importer
擅自改变。

## 12. Trial 完成和失败语义

| 结果 | 是否计分 | 是否可能重试 |
|---|---:|---:|
| verifier 返回 `reward=1` | 是，通过 | 否 |
| verifier 返回 `reward=0` | 是，未通过 | 否 |
| 达到 iteration cap，但 verifier 完成 | 是 | adapter 不因 cap 重试 |
| Agent API 或进程异常 | 取决于最终证据 | 是 |
| Agent timeout | 属于 Agent 预算结果，可继续验证残留状态 | 是 |
| verifier timeout | 否，无法判定 | 是 |
| 环境构建或启动失败 | 否，基础设施失败 | 是 |
| reward 缺失或不可解析 | 否，grader 失败 | 是 |

Trial 的 `finally` 路径负责关闭 bridge、停止或删除环境、记录结束时间并写
`result.json`。最终目录大致为：

```text
<trial-name>/
├── config.json
├── lock.json
├── result.json
├── exception.txt
├── trial.log
├── agent/
│   ├── buildmax-result.json
│   └── sessions/
├── verifier/
│   ├── reward.txt
│   ├── test-stdout.txt
│   └── test-stderr.txt
└── artifacts/
```

一个核心判断是：`reward=0` 表示 Agent 没做出来，是有效测量；环境没有启动、
verifier 没有产生 reward 才表示评测没有成功测量能力。

## 13. Job 聚合

所有 Trial 完成后，Harbor 聚合：

- 每个 Agent、模型和 dataset 的 rewards；
- 通过、未通过、异常和 retry 数量；
- token、费用和各阶段耗时；
- configured metrics 和 pass@k；
- Job 完成状态。

Harbor Job 是评测系统里的逻辑批次，不等于 Kubernetes Job。一个 Harbor Job 可以
包含很多 Trial，而每个 Trial 的 environment backend 又可能创建一个或多个运行时
资源。

## 14. BuildMax 导入阶段

`./make eval harbor run` 在 Harbor 结束后调用 BuildMax importer。即使 Harbor 在
中途失败，只要 Job 目录中存在已完成 Trial，运行器仍会尝试导入它们用于诊断。

Importer 读取：

```text
Harbor result.json
Harbor config.json
agent/buildmax-result.json
agent/sessions/
```

然后转换为 BuildMax 的 canonical trial bundle：

```text
.artifacts/evaluation/<experiment>/<subject>/
├── subject.json
├── experiment.json
└── trials/<task>/<attempt>/
    ├── bundle.json
    └── trace.jsonl
```

这个阶段不重新运行 Task、不调用模型，也不重新评分。它负责补充和统一：

- subject identity 和 artifact digest；
- 模型、transport、reasoning 和 policy；
- usage、cost、reply 和 trace；
- 稳定的 attempt index；
- 失败分类和 retention；
- 本地 suite 与外部 benchmark 共用的报告格式；
- candidate/baseline 的配对比较。

Harbor 不给 attempt 编号，只给带随机后缀的 Trial 名称。Importer 会按 Task、开始
时间和 Trial 名称稳定排序，再分配 attempt index，确保重复导入同一个 Job 时配对
关系不变。相关实现位于 [`job.go`](job.go)、[`convert.go`](convert.go) 和
[`import.go`](import.go)。

## 15. Oracle 流程

`--oracle` 将 BuildMax adapter 换成 Harbor 的 oracle Agent。Oracle 不调用模型，
而是把 Task 的 reference solution 上传到环境并执行，随后仍运行同一个 verifier。

因此 oracle 验证的是：

- Task 环境可以启动；
- reference solution 可以在该环境中执行；
- verifier 能够识别已知正确答案；
- 日志和 reward 通路正常。

Oracle 不证明 BuildMax 有解题能力，也不产生 BuildMax subject bundle。它适合在昂贵
的模型运行之前做环境 canary。

## 16. 映射到 Kubernetes

若 Environment 后端改为 Kubernetes，Harbor 上层生命周期原则上不变，只是环境
操作被翻译成 Kubernetes 资源：

| Harbor 操作 | Kubernetes 的可能实现 |
|---|---|
| 创建 Trial 隔离边界 | 每 Trial 一个 Namespace 或带唯一 label 的资源组 |
| 启动 `main` | Pod 或 Job |
| 启动 sidecar 服务 | 同 Pod sidecar，或独立 Deployment/Pod 加 Service |
| `exec` Agent/测试命令 | Pod exec，或独立 Agent/Verifier Job |
| 上传/下载文件 | volume、对象存储、init container 或 exec 流 |
| 健康检查 | readiness、主动探测和控制器等待 |
| 资源限制 | requests/limits、GPU resource、node selector |
| 网络策略 | NetworkPolicy 与出口代理 |
| 销毁环境 | 删除 Trial 资源或 Namespace |

“为每道题创建一个 Kubernetes Job”只覆盖其中一部分。若自研控制器替代 Harbor，
还必须明确接管：

- dataset 和 Task 解析；
- Task 版本与 checksum；
- 单容器和多容器环境物化；
- Agent 安装和执行协议；
- timeout、进程终止与 retry；
- tests 隐藏和 verifier 隔离；
- artifact、sidecar 状态和轨迹收集；
- reward 解析和失败分类；
- Job 聚合、pass@k 和证据格式。

因此 Kubernetes 是 placement 和隔离后端；Harbor 是 benchmark 语义与 Trial 生命周期
的解释器。两者解决的问题不同。

## 17. 维护检查点

升级 Harbor、切换 Terminal-Bench 版本或实现新 Environment 后端时，至少重新检查：

1. 自定义 Agent 基类及私有 helper 是否变化。
2. Task 格式、默认 timeout、verifier mode 和 reward 格式是否变化。
3. Dataset ref 是否仍为不可变引用。
4. 单容器、多容器、GPU 和网络策略能力是否与目标后端一致。
5. Agent timeout 是否真的终止容器内进程。
6. API key 是否仍会进入 Agent 可读文件系统。
7. Trial 结果文件名和目录布局是否变化。
8. retry 是否仍只针对异常，而不会把 `reward=0` 当作基础设施故障。
9. BuildMax 原始 envelope、Harbor result 和 importer 字段是否仍保持一致。
10. Oracle、单题 canary 和完成 Job 的导入是否全部通过。

本仓库的操作入口、参数和已知版本耦合继续以 [`README.md`](README.md) 为准；评测
合同、状态和长期架构边界以
[`../../docs/design/evaluation-system.md`](../../docs/design/evaluation-system.md)
为准。
