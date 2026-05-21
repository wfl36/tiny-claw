package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/wfl36/tiny-claw/internal/config"
	"github.com/wfl36/tiny-claw/internal/engine"
	"github.com/wfl36/tiny-claw/internal/feishu"
	"github.com/wfl36/tiny-claw/internal/provider"
	"github.com/wfl36/tiny-claw/internal/tools"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to YAML config")
	feishuMode := flag.Bool("feishu", false, "以飞书 WebSocket 长连接 daemon 模式运行 (需要 FEISHU_APP_ID / FEISHU_APP_SECRET)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [-config <path>] \"<你的指令>\"        # CLI 单次模式\n  %s [-config <path>] -feishu              # 飞书长连接 daemon 模式\n  echo \"<你的指令>\" | %s [-config <path>]\n\n", os.Args[0], os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	var p provider.LLMProvider
	switch cfg.Provider.Type {
	case "openai":
		p = provider.NewOpenAIProvider(cfg.Provider)
		log.Printf("[Main] Provider = OpenAI 兼容 (model=%s, base_url=%s)\n", cfg.Provider.Model, cfg.Provider.BaseURL)
	default:
		log.Fatalf("未知的 provider.type: %q (目前仅支持: openai)", cfg.Provider.Type)
	}

	workDir, _ := os.Getwd()

	registry := tools.NewRegistry()
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))
	registry.Register(tools.NewEditFileTool(workDir))

	eng := engine.NewAgentEngine(p, registry, workDir, false)

	if *feishuMode {
		runFeishuDaemon(cfg.Feishu, eng)
		return
	}

	prompt := readPrompt()
	if prompt == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := eng.Run(context.Background(), prompt, engine.NewTerminalReporter()); err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}
}

// runFeishuDaemon 进入飞书长连接守护模式,直到收到 SIGINT/SIGTERM 退出
func runFeishuDaemon(cfg config.FeishuConfig, eng *engine.AgentEngine) {
	bot := feishu.NewFeishuBot(cfg, eng)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[Main] 收到信号 %v,正在关闭飞书长连接...\n", sig)
		cancel()
	}()

	log.Println("🚀 飞书 WebSocket 长连接 daemon 启动,按 Ctrl+C 退出")
	if err := bot.StartWebSocket(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("飞书长连接异常退出: %v", err)
	}
	log.Println("📴 已退出")
}

// readPrompt 按优先级取指令: 位置参数优先,否则交互式读一行 stdin
//
// 注意: stdin 只读一行 (到 '\n' 即提交),所以
//   - shell 直接跑 / IDE Debug 跑 / `echo "xx" | claw` 都能用
//   - `claw < multi_line.txt` 只会取第一行 — 需要多行指令时请走位置参数
func readPrompt() string {
	if args := flag.Args(); len(args) > 0 {
		return strings.TrimSpace(strings.Join(args, " "))
	}
	if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
		_, _ = fmt.Fprint(os.Stderr, "> ")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}
