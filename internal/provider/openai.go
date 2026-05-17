package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"

	"github.com/wfl36/tiny-claw/internal/config"
	"github.com/wfl36/tiny-claw/internal/schema"
)

// OpenAIProvider 通过 OpenAI 兼容 API 实现 LLMProvider。
// 同一个实现可对接 OpenAI 官方、DeepSeek、通义、智谱等兼容端点,差异只在 BaseURL/Model
type OpenAIProvider struct {
	client openai.Client
	model  string
}

// NewOpenAIProvider 根据配置构造 Provider。client 内部维护连接池,可安全在多协程间复用
func NewOpenAIProvider(cfg config.ProviderConfig) *OpenAIProvider {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(cfg.Timeout))
	}
	return &OpenAIProvider{
		client: openai.NewClient(opts...),
		model:  cfg.Model,
	}
}

// Generate 把 schema 消息列表翻译为 OpenAI ChatCompletion 请求,并将响应还原回 schema.Message
func (p *OpenAIProvider) Generate(ctx context.Context, messages []schema.Message, availableTools []schema.ToolDefinition) (*schema.Message, error) {
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(p.model),
		Messages: toOpenAIMessages(messages),
	}
	// Thinking 阶段 engine 传 nil/空 — 此时不挂 Tools,模型物理上无法发起工具调用
	if len(availableTools) > 0 {
		params.Tools = toOpenAITools(availableTools)
	}

	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai chat.completions: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai response missing choices")
	}

	msg := resp.Choices[0].Message
	out := &schema.Message{
		Role:    schema.RoleAssistant,
		Content: msg.Content,
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, schema.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}
	return out, nil
}

func toOpenAIMessages(msgs []schema.Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case schema.RoleSystem:
			out = append(out, openai.SystemMessage(m.Content))

		case schema.RoleUser:
			// engine 把工具执行结果也存为 RoleUser + ToolCallID,要走 tool message 通道
			if m.ToolCallID != "" {
				out = append(out, openai.ToolMessage(m.Content, m.ToolCallID))
			} else {
				out = append(out, openai.UserMessage(m.Content))
			}

		case schema.RoleAssistant:
			if len(m.ToolCalls) == 0 {
				out = append(out, openai.AssistantMessage(m.Content))
				break
			}
			asst := openai.ChatCompletionAssistantMessageParam{
				ToolCalls: make([]openai.ChatCompletionMessageToolCallParam, 0, len(m.ToolCalls)),
			}
			if m.Content != "" {
				asst.Content.OfString = openai.String(m.Content)
			}
			for _, tc := range m.ToolCalls {
				asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				})
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &asst})
		}
	}
	return out
}

func toOpenAITools(defs []schema.ToolDefinition) []openai.ChatCompletionToolParam {
	out := make([]openai.ChatCompletionToolParam, 0, len(defs))
	for _, d := range defs {
		fn := shared.FunctionDefinitionParam{Name: d.Name}
		if d.Description != "" {
			fn.Description = openai.String(d.Description)
		}
		// InputSchema 期望是 map[string]any 形态的 JSON Schema
		if m, ok := d.InputSchema.(map[string]any); ok {
			fn.Parameters = shared.FunctionParameters(m)
		}
		out = append(out, openai.ChatCompletionToolParam{Function: fn})
	}
	return out
}
