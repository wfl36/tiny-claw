package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/wfl36/tiny-claw/internal/config"
	"github.com/wfl36/tiny-claw/internal/engine"
	"github.com/wfl36/tiny-claw/internal/provider"
	"github.com/wfl36/tiny-claw/internal/schema"
)

// 升级版 Mock Provider
type mockProvider struct {
	turn int
}

func (m *mockProvider) Generate(ctx context.Context, msgs []schema.Message, tools []schema.ToolDefinition) (*schema.Message, error) {
	// 如果工具列表为空，说明这是引擎发起的 Phase 1: Thinking 阶段
	if len(tools) == 0 {
		return &schema.Message{
			Role:    schema.RoleAssistant,
			Content: "【推理中】目标是检查文件。我不能直接盲猜，我需要先调用 bash 工具执行 ls 命令，看看当前目录下有什么，然后再做定夺。",
		}, nil
	}

	// 如果工具列表不为空，说明这是 Phase 2: Action 阶段
	m.turn++
	if m.turn == 1 {
		// 第一轮 Action：顺着刚才的 Thinking，精准调用工具
		return &schema.Message{
			Role:    schema.RoleAssistant,
			Content: "我要执行我刚才计划的步骤了。",
			ToolCalls: []schema.ToolCall{
				{ID: "call_123", Name: "bash", Arguments: []byte(`{"command": "ls -la"}`)},
			},
		}, nil
	}

	// 第二轮 Action：直接总结退出
	return &schema.Message{
		Role:    schema.RoleAssistant,
		Content: "根据工具返回的结果，我看到了 main.go，任务圆满完成！",
	}, nil
}

type mockRegistry struct{}

func (m *mockRegistry) GetAvailableTools() []schema.ToolDefinition {
	// 给 bash 工具一个最小可用的 JSON Schema, 真实 LLM 才能据此生成合法 arguments
	return []schema.ToolDefinition{
		{
			Name:        "bash",
			Description: "Execute a shell command in the workspace and return stdout/stderr.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The shell command to execute.",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

func (m *mockRegistry) Execute(ctx context.Context, call schema.ToolCall) schema.ToolResult {
	return schema.ToolResult{
		ToolCallID: call.ID,
		Output:     "-rw-r--r--  1 user group  234 Oct 24 10:00 main.go\n",
		IsError:    false,
	}
}

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		// 离线开箱即用: 没写配置时退回 mock,方便先把 engine 闭环跑通
		if os.IsNotExist(err) || errors.Is(err, os.ErrNotExist) {
			log.Printf("[Main] 未找到 %s, 退回 mock 模式 (离线 smoke test)\n", *configPath)
			cfg = &config.Config{Provider: config.ProviderConfig{Type: "mock"}}
		} else {
			log.Fatalf("加载配置失败: %v", err)
		}
	}

	// 根据 config 选 Provider — 这是把 mock 替换为真实 LLM 的唯一切换点
	var p provider.LLMProvider
	switch cfg.Provider.Type {
	case "openai":
		p = provider.NewOpenAIProvider(cfg.Provider)
		log.Printf("[Main] Provider = OpenAI 兼容 (model=%s, base_url=%s)\n", cfg.Provider.Model, cfg.Provider.BaseURL)
	case "mock", "":
		p = &mockProvider{}
		log.Println("[Main] Provider = mock (离线脚本)")
	default:
		log.Fatalf("未知的 provider.type: %q (支持: openai / mock)", cfg.Provider.Type)
	}

	r := &mockRegistry{}
	workDir, _ := os.Getwd()

	// 实例化引擎，开启 EnableThinking = true
	eng := engine.NewAgentEngine(p, r, workDir, true)

	if err := eng.Run(context.Background(), "帮我检查当前目录的文件"); err != nil {
		log.Fatalf("引擎崩溃: %v", err)
	}
}
