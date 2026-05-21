# tiny-claw

一个最小可用的 AI Coding Agent，用 Go 实现。目标是用最少的代码量讲清楚现代 Coding Agent 的核心 Main Loop：

**LLM 推理 → 工具调用 → 物理 IO → 结果回灌 → 下一轮推理**

## 核心特性

- **OpenAI 兼容 Provider**：一份实现接通 OpenAI / DeepSeek / OpenRouter / 通义 / 智谱等所有兼容端点，差异只在 `base_url` + `model`
- **可选的 Thinking + Action 双阶段循环**：开启后会先剥夺工具让模型纯文本规划，再恢复工具让其精准行动；对原生 function calling 支持好的模型（如 deepseek 系、OpenAI 系）尤其干净
- **路由式 Tool Registry**：基于 `BaseTool` 接口；模型幻觉工具名 / 参数解析失败 / 执行报错全部以 `IsError=true` 形态回灌，让模型自己纠错而不是中断主循环
- **极简工具集**：`read_file` / `write_file` / `bash` / `edit_file` 四件套，覆盖一般编程任务所需的最小动作集
- **工作区约束**：所有 IO 工具锁定在 `WorkDir` 下；`bash` 30s 强制超时；8KB 输出截断防 Context 爆炸
- **Reporter 抽象的多形态输出**：终端 / 飞书机器人共用同一份引擎，新增渠道只需实现 `Reporter` 四个回调
- **飞书 WebSocket 长连接接入**：走官方 [oapi-sdk-go/v3/ws](https://open.feishu.cn/document/server-docs/server-side-sdk) 长连接，无需公网 IP / 回调 URL / encrypt key，凭据放在 `config.yaml`

## 项目结构

```
tiny-claw/
├── cmd/claw/main.go                    # CLI 入口: 装配 provider + registry + engine,支持 -feishu daemon
├── internal/
│   ├── engine/
│   │   ├── loop.go                     # Main Loop: Thinking + Action 双阶段实现
│   │   ├── reporter.go                 # Reporter 接口 (OnThinking / OnToolCall / OnToolResult / OnMessage)
│   │   └── terminal_reporter.go        # CLI 模式的 stdout 渲染实现
│   ├── feishu/bot.go                   # 飞书长连接 (WebSocket) + FeishuReporter
│   ├── provider/
│   │   ├── interface.go                # LLMProvider 接口 (依赖倒置)
│   │   └── openai.go                   # OpenAI 兼容实现 (官方 openai-go SDK)
│   ├── tools/
│   │   ├── registry.go                 # BaseTool 接口 + map 路由的 Registry
│   │   ├── read_file.go                # 读文件 + 8KB 截断
│   │   ├── write_file.go               # 写文件 + 自动 mkdir-p
│   │   ├── bash.go                     # bash -c + 30s 超时 + 错误自愈
│   │   └── edit_file.go                # 四级降级模糊替换 (exact → CRLF → trim → indent-tolerant)
│   ├── schema/message.go               # 统一的 Message / ToolCall / ToolResult / ToolDefinition
│   └── config/config.go                # YAML 配置加载 (provider + feishu)
├── configs/
│   └── config.example.yaml             # 配置示例 (实际 config.yaml 已 gitignored)
├── go.mod
└── README.md
```

## 快速开始

### 1. 安装依赖

```bash
go mod download
```

### 2. 写入配置

```bash
cp configs/config.example.yaml configs/config.yaml
```

编辑 `configs/config.yaml`，填入对应厂商的 `base_url` / `api_key` / `model`。常见端点：

| 厂商 | base_url |
|---|---|
| OpenAI 官方 | `https://api.openai.com/v1` |
| DeepSeek | `https://api.deepseek.com/v1` |
| OpenRouter | `https://openrouter.ai/api/v1` |
| 通义千问 | `https://dashscope.aliyuncs.com/compatible-mode/v1` |
| 智谱 GLM | `https://open.bigmodel.cn/api/paas/v4` |

如果要跑飞书 daemon，再填一段 `feishu`：

```yaml
feishu:
  app_id: cli_xxxxxxxxxxxx
  app_secret: xxxxxxxxxxxxxxxxxxxxxxxx
  verify_token: ""   # 长连接模式留空
  encrypt_key: ""    # 长连接模式留空
```

### 3. 跑一条指令 (CLI 单次模式)

支持三种输入方式：

```bash
# 位置参数 (单行)
go run ./cmd/claw "请读取 cmd/claw/main.go 并总结它的入口流程"

# 管道
echo "列出当前目录下所有 .go 文件" | go run ./cmd/claw

# IDE Debug: 启动后在控制台输入一行,回车即提交
```

指定其它配置文件：`go run ./cmd/claw -config path/to/config.yaml "..."`

### 4. 飞书机器人 daemon 模式

```bash
go run ./cmd/claw -feishu
```

启动后看到 `connected to wss://msg-frontier.feishu.cn/...` 即表示长连接握手成功；之后在飞书里 @机器人 或私聊它，引擎会把每一步 Thinking / Tool / Result / Message 回放到当前会话。`Ctrl+C` 优雅退出。

需要在 [飞书开放平台](https://open.feishu.cn/app) 后台为该应用：
- **事件订阅** → 选 "使用长连接接收事件"
- 订阅 `im.message.receive_v1` 事件
- 在 **权限管理** 开通 `im:message`、`im:message.group_at_msg`、`im:message:send_as_bot` 等相关 scope

## 内置工具

| 名称 | 用途 | 关键参数 | 关键防线 |
|---|---|---|---|
| `read_file` | 读 workDir 相对路径的文件 | `path` | 8KB 截断 |
| `write_file` | 创建 / 覆盖文件，自动建父目录 | `path`, `content` | 限定 workDir |
| `bash` | 在 workDir 下执行 bash 命令 | `command` | 30s 超时，stderr 当内容回传，8KB 截断 |
| `edit_file` | 字符串级别的局部替换 | `path`, `old_text`, `new_text` | 四级降级模糊匹配，歧义报错让模型重试 |

所有工具都实现 `BaseTool` 接口（`Name() / Definition() / Execute()`），在 `cmd/claw/main.go` 中通过 `registry.Register()` 挂载。**新增一个工具不需要改 engine 或 provider 任何代码** —— 这是依赖倒置的核心收益。

## 架构要点

- **engine 与 provider 解耦**：`engine` 只依赖 `provider.LLMProvider` 接口；接入任何新模型只需写一个实现 `Generate` 的类型
- **engine 与展现层解耦**：`engine` 通过 `Reporter` 接口对外吐事件；终端 (`TerminalReporter`) 与飞书 (`FeishuReporter`) 是两个并行实现，新增一个渠道 = 新增一个文件
- **工具调用结果回灌**：tool result 走 `RoleUser` + `ToolCallID` 通道（OpenAI 风格），`provider/openai.go` 内部映射为 `ToolMessage`
- **Thinking 阶段的开关**：`engine.NewAgentEngine(..., enableThinking)` 的最后一个参数控制是否开启慢思考；`openai.go` 在 `tools` 为空时不传 `Tools` 字段，从协议层面阻止模型调用工具
- **错误自纠错**：工具执行失败不会 panic / Fatalf，错误信息原样作为 `ToolResult.Output` + `IsError=true` 回给模型，让模型在下一轮自己读、自己改
- **飞书长连接守护**：`feishu.FeishuBot.StartWebSocket(ctx)` 调用官方 SDK 的 `ws.NewClient`，开 `AutoReconnect`；每一条入站消息 fork 出独立 goroutine 跑 Agent，避免阻塞事件回调

## License

MIT
