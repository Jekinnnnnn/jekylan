## 项目概述

jekylan 是一个 LLM 对话引擎框架，封装了多 Provider LLM 抽象、工具系统、Skill 系统、Memory 系统，以及 **Agent 编排与 Playbook 工作流** 能力。支持 Anthropic 和 OpenAI 双 Provider。

## 启动

1. 配置 API Key：`export JEKYLAN_API_KEY="xxxxxxxxxx"`
2. 编译：`go build -o jekylan ./cmd/jekylan`
3. 启动对话：`./jekylan -config ./openai.yaml`

## 组件介绍

### 1. Agent 系统 (`internal/agent`)

- **Markdown 驱动定义**：`agents/*.md` 文件即 Agent 定义，YAML frontmatter 描述元数据（name/description/tools/max_turns/model），正文为 system prompt，无需代码注册
- **单 goroutine 事件循环**：Coordinator 采用单 goroutine + channel 事件队列模式管理所有 Agent 状态，完全无锁，避免并发竞争
- **生命周期闭环**：spawn / wait / kill / confirm 四态管理，每个 Agent 独立 `context.CancelFunc`，Kill 时精准取消而不会波及其他 Agent
- **桥接层解耦**：Spawner 实现 `AgentSpawner` 接口，Playbook Executor 仅依赖接口而非具体实现，方便测试和扩展
- **确认门控**：敏感工具调用前自动触发 confirm，用户输入 `"确认"` 或 `/confirm <agent-id> yes` 后放行，支持多 Agent 并发等待确认时的 FIFO 队列排序
- **Token 无损透传**：子 Agent usage 通过 `usageSink -> AddUsage` 实时累加到父 Engine 的总会话统计，不遗漏任何子代理消耗
- **调试门控**：配置 `debug: true` 时，Coordinator 的 runner event consumption goroutine 实时将子 Agent streaming text 输出到 stderr，便于追踪多 Agent 协作时的完整思考过程
- **EngineDriver 隔离**：Engine REPL 循环跑在独立 goroutine 中，通过 Input()/Output()/Done() channel 桥接 Coordinator，避免阻塞主事件循环
- **可插拔 ResultConsumer**：不同 Agent 类型可注入自定义结果消费策略，默认 `DefaultResultConsumer` 从 `RunEventComplete` 提取结果文本
- **Confirm 转发 goroutine**：每个 Agent 启动独立的 confirm 转发 goroutine，将 runner 的 confirm 请求异步注入 Coordinator 事件队列，避免 runner 阻塞

### 2. Playbook 系统 (`internal/playbook`)

- **Markdown 声明式定义**：`playbooks/*.md` 文件即工作流定义，YAML frontmatter 描述元数据（name/description/when_to_use），正文为自然语言步骤列表，降低编写门槛
- **两层编排结构**：Phase / Step 两层抽象，Phase 决定串行或并行，Step 描述具体操作，既支持简单线性流程也支持复杂并行计算
- **变量插值系统**：`${var_name}` 语法跨步骤传递数据，Executor 维护全局变量表，自动替换后续 step 的 prompt 中的占位符，实现步骤间数据管道
- **条件触发**：`when_to_use` 字段描述触发条件，由 LLM 自主判断用户意图是否匹配，无需硬编码路由规则，支持模糊匹配
- **Agent 类型绑定**：每个 step 可通过 `agent:` 字段指定 Agent 类型，复用 `agents/` 目录下的定义，实现不同步骤由不同专家 Agent 处理
- **确认机制打通**：step 级别 `confirm: true` 与 Agent 确认门控无缝集成，用户可在任意步骤暂停审查中间结果
- **并行失败回滚**：并行 phase 中若某 step spawn 失败，Executor 自动 Kill 已创建的同 phase 其他 Agent，避免孤儿进程和资源泄漏
- **工具化暴露**：`playbooktool` 将 Playbook 暴露为普通工具，LLM 可像调用 bash/file_read 一样自主选择和执行工作流，实现上层策略与下层执行的解耦
- **输出变量绑定**：每个 step 的 `output:` 将 Agent 执行结果绑定到变量名，后续步骤可直接引用，形成可追溯的数据链

### 3. 多 Provider LLM 抽象层 (`internal/llm`)

- **统一 Client 接口**：`StreamMessages` 方法屏蔽 Anthropic/OpenAI 差异
- **工厂模式创建**：`Factory.NewClient` 根据配置自动选择 Provider
- **模型降级回退**：Anthropic 过载时自动切换到 fallback model
- **Token 计数接口**：Anthropic 使用原生 CountTokens API，OpenAI 回退到估算

### 4. Skill 系统 (`internal/skill`)

- **SKILL.md 驱动**：每个技能一个目录 + SKILL.md 文件，YAML frontmatter 定义元数据
- **工具白名单**：`allowed-tools` 限制技能可调用的工具范围
- **模型覆盖**：技能可指定自己的 model，不强制使用全局配置
- **用户可调用标记**：`user-invocable` 控制是否暴露给用户
- **动态加载**：`LoadDir` 扫描目录自动注册所有技能

### 5. Memory 系统 (`internal/memory`)

- **基于文件的持久化**：MEMORY.md 索引 + 分散的 topic 文件
- **四种记忆类型**：user / feedback / project / reference，各有明确的 when_to_save / how_to_use
- **记忆召回**：
  - `KeywordSelector`：关键词匹配（filename + description）
  - `LLMSelector`：大模型语义匹配
- **Save Rules 条件控制**：`save-rules/*.yaml` 配置 per-type 的 save/skip 条件
- **自动截断**：MEMORY.md 超过 200 行或 25KB 时自动截断并附加警告

### 6. Engine 层 (`internal/engine`)

- **对话循环**：REPL 模式和单次模式双支持
- **记忆注入**：每轮查询前自动召回相关记忆文件，注入为 user context message
- **Session 持久化**：`WithSessionPath` 支持对话历史的保存/恢复（JSON 格式）
- **Token 统计**：跨轮次累加 input/output/cache_create/cache_read
- **Orphan 修复**：`/stop` 或崩溃后自动修复未完成的 tool_use → tool_result
- **Coordinator 模式**：启用后 Engine 作为协调器运行，管理子 Agent 而非直接处理任务

### 7. Context Compact 系统 (`internal/compact`)

- **四级压缩管线**：
  1. **Microcompact**：基于 token 阈值的消息级别压缩
  2. **Auto-compact**：LLM 驱动的智能摘要压缩
  3. **Reactive compact**：PromptTooLong 时的紧急压缩
- **Token 预算管理**：`CalculateTokenWarningState` 计算警告/阻塞状态
- **Compaction Result 同步**：压缩完成后通过 `compaction_result` event 同步回 Engine

### 8. Query 引擎 (`internal/query`)

- **Streaming 架构**：`<-chan Event` 输出文本增量、工具调用、使用统计等事件
- **工具并行执行**：多个 tool_use 结果通过 `sync.WaitGroup` + channel 并行处理
- **PromptTooLong 自动恢复**：检测到 PTL 后自动触发 reactive compact 并重试
- **Max Output Tokens 恢复**：输出截断时自动插入恢复消息继续生成
- **Context 取消支持**：`ctx.Err()` 检查实现 `/stop` 中断

### 9. 工具系统 (`internal/tool`)

- **注册表模式**：`tool.Registry` 统一管理，支持按名查找
- **文件状态跟踪**：`FileStateTracker` 记录读写过的文件
- **Skill 工具**：skill 调用
- **Workflow Complete 工具**：`workflow_complete` 标记 workflow skill 结束
- **Confirm 工具**：用户确认门控

### 10. 消息格式 (`internal/message`)

- **多模态内容块**：TextBlock / ToolUseBlock / ToolResultBlock / ThinkingBlock / RedactedThinkingBlock
- **Usage 统计**：input/output/cache_create/cache_read 四级 token 统计
- **ResponseID 追踪**：跨 Provider 统一的消息 ID

## 架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                            User Input                                │
└─────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         Engine (REPL loop)                           │
│  ├─ 主 goroutine: 事件循环 (input / notify / usage / events)        │
│  ├─ 单次/连续查询管理                                                │
│  ├─ Token 累加 (totalUsage)                                          │
│  └─ Session 保存/恢复                                                │
└─────────────────────────────────────────────────────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              ▼                    ▼                    ▼
    ┌──────────────┐    ┌──────────────────┐   ┌──────────────┐
    │  Memory      │    │  Query Engine    │   │  Coordinator │
    │  (recall)    │    │  (LLM stream)    │   │  (agent mgr) │
    └──────────────┘    └──────────────────┘   └──────┬───────┘
                                                      │
                          ┌───────────────────────────┼───────────┐
                          ▼                           ▼           ▼
                   ┌────────────┐            ┌────────────┐  ┌──────────┐
                   │  Playbook  │            │  Agent-0   │  │ Agent-1  │
                   │  Executor  │            │  Runner    │  │ Runner   │
                   │            │            └────────────┘  └──────────┘
                   │  变量插值  │
                   │  步骤编排  │
                   │  并行/串行 │
                   └────────────┘
```

### Agent + Playbook 协作流程

1. **触发**：用户输入匹配 Playbook 的 `when_to_use`，或 LLM 主动调用 `playbook` 工具
2. **解析**：Playbook 解析为 `ExecutionPlan`（phase/step 结构）
3. **执行**：`playbook.Executor` 按 phase 顺序执行
   - 每个 step 通过 `AgentSpawner.Spawn()` 创建子 Agent
   - step 的 `prompt` 支持 `${var}` 变量插值
   - `confirm: true` 时等待用户确认后继续
4. **结果收集**：`Spawner.Wait()` 阻塞等待 Agent 完成，结果写入变量表
5. **汇总**：所有子 Agent 的 token usage 通过 `usageSink -> AddUsage` 累加到主会话统计

## 配置说明

配置示例见 `openai.yaml`、`anthropic.yaml`。

| 配置项 | 说明 |
|--------|------|
| `provider` | LLM Provider：`anthropic` 或 `openai` |
| `model` | 模型名称 |
| `base_url` | 自定义 API 基础地址 |
| `max_turns` | 单轮最大对话轮次 |
| `thinking_budget` | 思考预算（Anthropic） |
| `tools` | 启用的工具列表 |
| `skills_dir` | Skill 目录路径 |
| `agents_dir` | Agent 定义目录路径（默认：`agents`） |
| `playbook_dir` | Playbook 定义目录路径（默认：`playbooks`） |
| `debug` | 是否打印子 Agent 的 streaming 输出 |
| `enable_memory` / `memory_dir` | 记忆系统开关和目录 |
| `session_file` | 会话持久化文件路径 |
| `disable_compact` | 关闭压缩 |
| `api_max_input_tokens` / `api_target_input_tokens` | API 压缩触发阈值和目标阈值 |
