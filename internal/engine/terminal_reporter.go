package engine

import (
	"context"
	"fmt"
)

// TerminalReporter 把引擎的事件渲染到 stdout,用于本地 CLI 模式
type TerminalReporter struct{}

func NewTerminalReporter() *TerminalReporter { return &TerminalReporter{} }

func (TerminalReporter) OnThinking(ctx context.Context) {
	fmt.Println("🤔 [Thinking] 模型正在慢思考...")
}

func (TerminalReporter) OnToolCall(ctx context.Context, toolName string, args string) {
	fmt.Printf("🛠️  [Tool] %s args=%s\n", toolName, args)
}

func (TerminalReporter) OnToolResult(ctx context.Context, toolName string, result string, isError bool) {
	if isError {
		fmt.Printf("⚠️  [Tool:%s] 报错: %s\n", toolName, result)
		return
	}
	fmt.Printf("✅ [Tool:%s] 成功 (%d bytes)\n", toolName, len(result))
}

func (TerminalReporter) OnMessage(ctx context.Context, content string) {
	fmt.Printf("🤖 %s\n", content)
}

var _ Reporter = (*TerminalReporter)(nil)
