// Package config provides YAML-backed configuration loading.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config 是 tiny-claw 的根配置
type Config struct {
	Provider ProviderConfig `yaml:"provider"`
}

// ProviderConfig 描述 LLM Provider 的连接参数
type ProviderConfig struct {
	Type    string        `yaml:"type"`     // "openai" | "mock"
	BaseURL string        `yaml:"base_url"` // 兼容端点 URL,例如 https://api.deepseek.com/v1
	APIKey  string        `yaml:"api_key"`
	Model   string        `yaml:"model"`   // 例如 gpt-4o-mini / deepseek-chat
	Timeout time.Duration `yaml:"timeout"` // 单次请求超时,默认 60s
}

// Load 从 YAML 文件读取配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if c.Provider.Timeout == 0 {
		c.Provider.Timeout = 60 * time.Second
	}
	return &c, nil
}
