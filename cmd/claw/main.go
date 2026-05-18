package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/wfl36/tiny-claw/internal/config"
	"github.com/wfl36/tiny-claw/internal/engine"
	"github.com/wfl36/tiny-claw/internal/provider"
	"github.com/wfl36/tiny-claw/internal/tools"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to YAML config")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  %s [-config <path>] \"<你的指令>\"\n  echo \"<你的指令>\" | %s [-config <path>]\n\n", os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	prompt := readPrompt()
	if prompt == "" {
		flag.Usage()
		os.Exit(2)
	}

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

	// 初始化真实的 Tool Registry
	registry := tools.NewRegistry()

	// 挂载极简工具集
	registry.Register(tools.NewReadFileTool(workDir))
	registry.Register(tools.NewWriteFileTool(workDir))
	registry.Register(tools.NewBashTool(workDir))
	registry.Register(tools.NewEditFileTool(workDir))

	// 实例化引擎
	eng := engine.NewAgentEngine(p, registry, workDir, false)

	if err := eng.Run(context.Background(), prompt); err != nil {
		log.Fatalf("引擎运行崩溃: %v", err)
	}
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
	// 只在 stdin 是真终端时打印提示符,管道场景不污染日志
	if stat, err := os.Stdin.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
		_, _ = fmt.Fprint(os.Stderr, "> ")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}
