## 项目概述

jekylan 是一个 Harness 框架，封装了 LLM 对话引擎、工具系统、Skill 系统和 Memory 系统，支持 Anthropic 和 OpenAI 双 Provider。

## 启动
1. 在环境变量配置api key：JEKYLAN_API_KEY="xxxxxxxxxx"
2. 开始对话：./jekylan -config ./openai.yaml

## 组件介绍

### 1. 多 Provider LLM 抽象层 (`internal/llm`)

- **统一 Client 接口**：`StreamMessages` 方法屏蔽 Anthropic/OpenAI 差异
- **工厂模式创建**：`Factory.NewClient` 根据配置自动选择 Provider
- **模型降级回退**：Anthropic 过载时自动切换到 fallback model
- **Token 计数接口**：Anthropic 使用原生 CountTokens API，OpenAI 回退到估算

### 2. Skill 系统 (`internal/skill`)

- **SKILL.md 驱动**：每个技能一个目录 + SKILL.md 文件，YAML frontmatter 定义元数据
- **工具白名单**：`allowed-tools` 限制技能可调用的工具范围
- **模型覆盖**：技能可指定自己的 model，不强制使用全局配置
- **用户可调用标记**：`user-invocable` 控制是否暴露给用户
- **动态加载**：`LoadDir` 扫描目录自动注册所有技能

### 3. Memory 系统 (`internal/memory`)

- **基于文件的持久化**：MEMORY.md 索引 + 分散的 topic 文件
- **四种记忆类型**：user / feedback / project / reference，各有明确的 when_to_save / how_to_use
- **记忆召回**：
  - `KeywordSelector`：关键词匹配（filename + description）
  - `LLMSelector`：大模型语义匹配
- **Save Rules 条件控制**：`save-rules/*.yaml` 配置 per-type 的 save/skip 条件，注入 prompt 指导 LLM 判断
- **自动截断**：MEMORY.md 超过 200 行或 25KB 时自动截断并附加警告

### 4. SkillCollector — 程序化 Skill 执行分析 (`internal/engine/skillcollector`)

- **阈值触发**：配置 `threshold`，达到 N 次执行后自动运行分析 LLM
- **Workflow 模式**：`workflow_mode: true` 支持跨多轮 SubmitMessage 跟踪，直到 `workflow_complete`
- **自动总结**：`auto_summarize` 在执行片段上调用 LLM 生成 Context，解决长流程分析问题
- **消息范围隔离**：`StartMessageIndex` / `EndMessageIndex` 精确定位执行范围，避免深拷贝

### 5. Engine 层 — 记忆注入与 Selector 路由 (`internal/engine`)

- **Workflow-Aware Selector**：`workflowAwareSelector` 在 memory recall 中强制注入 active workflow 的 feedback，解决后续轮次关键词不匹配问题
- **可注入的 Selector Builder**：`memorySelectorBuilder` 支持自定义路由逻辑，默认根据 activeWorkflow 动态选择 KeywordSelector 或 workflowAwareSelector
- **Prompt 统一管理**：`prompt.go` 集中管理系统 prompt 各节（Intro/Tasks/Actions/Tools/Tone/Environment/Memory）

### 6. Context Compact 系统 (`internal/compact`)

- **四级压缩管线**：
  1. **Microcompact**：基于 token 阈值的消息级别压缩
  2. **Auto-compact**：LLM 驱动的智能摘要压缩
  3. **Reactive compact**：PromptTooLong 时的紧急压缩
- **Token 预算管理**：`CalculateTokenWarningState` 计算警告/阻塞状态，防止超限
- **Compaction Result 同步**：压缩完成后通过 `compaction_result` event 同步回 Engine

### 7. Query 引擎 — 多轮对话循环 (`internal/query`)

- **Streaming 架构**：`<-chan Event` 输出文本增量、工具调用、使用统计等事件
- **工具并行执行**：多个 tool_use 结果通过 `sync.WaitGroup` + channel 并行处理
- **PromptTooLong 自动恢复**：检测到 PTL 后自动触发 reactive compact 并重试
- **Max Output Tokens 恢复**：输出截断时自动插入恢复消息继续生成
- **Context 取消支持**：`ctx.Err()` 检查实现 `/stop` 中断

### 8. 工具系统 (`internal/tool`)

- **注册表模式**：`tool.Registry` 统一管理，支持按名查找
- **文件状态跟踪**：`FileStateTracker` 记录读写过的文件，避免重复操作
- **Skill工具**： skill 调用
- **Workflow Complete 工具**：`workflow_complete` 标记 workflow skill 结束

### 9. 消息格式 (`internal/message`)

- **多模态内容块**：TextBlock / ToolUseBlock / ToolResultBlock / ThinkingBlock / RedactedThinkingBlock
- **Usage 统计**：input/output/cache_create/cache_read 四级 token 统计
- **ResponseID 追踪**：跨 Provider 统一的消息 ID

### 10. 配置与持久化

- **YAML 配置**：`config.yaml` 统一管理 Provider/Model/Tools/Compact/Memory 等配置
- **Session 持久化**：`WithSessionPath` 支持对话历史的保存/恢复
