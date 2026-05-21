// Package feishu 把 tiny-claw 引擎接到飞书机器人事件流。
// 采用飞书官方推荐的 WebSocket 长连接 (无需公网回调 URL)，
// 参考: https://open.feishu.cn/document/server-docs/server-side-sdk
package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/wfl36/tiny-claw/internal/config"
	"github.com/wfl36/tiny-claw/internal/engine"
)

// FeishuBot 把飞书凭据、客户端、Agent 引擎打成一个工作单元
type FeishuBot struct {
	client *lark.Client
	cfg    config.FeishuConfig
	engine *engine.AgentEngine
}

// NewFeishuBot 从 config 读取凭据,构建 lark client + 持引擎引用
func NewFeishuBot(cfg config.FeishuConfig, eng *engine.AgentEngine) *FeishuBot {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		log.Fatal("config.yaml 中缺少 feishu.app_id 或 feishu.app_secret")
	}

	return &FeishuBot{
		client: lark.NewClient(cfg.AppID, cfg.AppSecret),
		cfg:    cfg,
		engine: eng,
	}
}

// StartWebSocket 启动飞书官方 SDK 的 WebSocket 长连接。
// 长连接模式由飞书服务端主动推事件,客户端不需要任何公网 IP / 回调地址 / encrypt key。
// 调用阻塞直到 ctx 被取消或底层连接彻底失败。
func (b *FeishuBot) StartWebSocket(ctx context.Context) error {
	// 长连接走的是认证通道,verify token / encrypt key 都不需要,留空即可
	eventHandler := b.buildDispatcher("", "")

	wsClient := ws.NewClient(
		b.cfg.AppID,
		b.cfg.AppSecret,
		ws.WithEventHandler(eventHandler),
		ws.WithLogLevel(larkcore.LogLevelInfo),
		ws.WithAutoReconnect(true),
		ws.WithOnReady(func() {
			log.Println("[Feishu] ✅ WebSocket 长连接就绪,开始接收事件...")
		}),
		ws.WithOnDisconnected(func() {
			log.Println("[Feishu] ⚠️ WebSocket 已断开,SDK 将尝试自动重连")
		}),
		ws.WithOnError(func(err error) {
			log.Printf("[Feishu] WS 错误: %v\n", err)
		}),
	)

	log.Println("[Feishu] 正在连接飞书 WebSocket 服务器...")
	return wsClient.Start(ctx)
}

// GetEventDispatcher 暴露 HTTP 回调用调度器 (备用方案,需要公网 callback)。
// 推荐 StartWebSocket 路径。
func (b *FeishuBot) GetEventDispatcher() *dispatcher.EventDispatcher {
	return b.buildDispatcher(b.cfg.VerifyToken, b.cfg.EncryptKey)
}

// buildDispatcher 把"接收消息事件"绑定到 Agent 任务,WS 与 HTTP 共用同一份业务逻辑
func (b *FeishuBot) buildDispatcher(verifyToken, encryptKey string) *dispatcher.EventDispatcher {
	return dispatcher.NewEventDispatcher(verifyToken, encryptKey).
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			// 飞书消息 content 是个 JSON 字符串,这里只取最常见的 text 类型
			text := extractText(*event.Event.Message.Content)
			chatId := *event.Event.Message.ChatId
			log.Printf("[Feishu] 收到会话 %s 消息: %s\n", chatId, text)

			// 每个会话独立 goroutine,不能阻塞事件回调
			go b.handleAgentRun(chatId, text)
			return nil
		}).
		OnP2MessageReadV1(func(ctx context.Context, event *larkim.P2MessageReadV1) error {
			// 已读回执 — 静默忽略
			return nil
		})
}

// extractText 把 `{"text":"..."}` 这种 content 字符串解出来,失败则原样返回
func extractText(raw string) string {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err == nil && payload.Text != "" {
		return strings.TrimSpace(payload.Text)
	}
	return raw
}

// handleAgentRun 把消息转交给 Agent 引擎,并把回放通道指向当前会话
func (b *FeishuBot) handleAgentRun(chatId string, prompt string) {
	reporter := &FeishuReporter{client: b.client, chatId: chatId}
	if err := b.engine.Run(context.Background(), prompt, reporter); err != nil {
		reporter.sendMsg(fmt.Sprintf("❌ Agent 运行崩溃: %v", err))
	}
}

// ==========================================
// FeishuReporter: 引擎事件 → 飞书消息
// ==========================================

type FeishuReporter struct {
	client *lark.Client
	chatId string
}

func (r *FeishuReporter) sendMsg(text string) {
	contentBytes, _ := json.Marshal(map[string]string{"text": text})

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(r.chatId).
			MsgType(larkim.MsgTypeText).
			Content(string(contentBytes)).
			Build()).
		Build()

	if _, err := r.client.Im.Message.Create(context.Background(), req); err != nil {
		log.Printf("[Feishu] 发送消息失败: %v\n", err)
	}
}

func (r *FeishuReporter) OnThinking(ctx context.Context) {
	r.sendMsg("🤔 模型正在慢思考 (Thinking)...")
}

func (r *FeishuReporter) OnToolCall(ctx context.Context, toolName string, args string) {
	r.sendMsg(fmt.Sprintf("🛠️ 正在执行工具: %s\n参数: %s", toolName, args))
}

func (r *FeishuReporter) OnToolResult(ctx context.Context, toolName string, result string, isError bool) {
	if isError {
		r.sendMsg(fmt.Sprintf("⚠️ 工具报错 (%s):\n%s", toolName, result))
		return
	}
	r.sendMsg(fmt.Sprintf("✅ 工具执行成功 (%s)", toolName))
}

func (r *FeishuReporter) OnMessage(ctx context.Context, content string) {
	r.sendMsg(content)
}

var _ engine.Reporter = (*FeishuReporter)(nil)
