// Package config 负责三层配置解析：启动参数 > 环境变量 > YAML 配置文件。
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 18081

	DefaultDeepSeekBaseURL  = "https://api.deepseek.com"
	DefaultBochaBaseURL     = "https://api.bocha.cn"
	DefaultContext7Endpoint = "https://mcp.context7.com/mcp"
	DefaultGrepAppEndpoint  = "https://mcp.grep.app"
)

// Config 是服务的运行配置。
type Config struct {
	DeepSeekAPIKey string
	BochaAPIKey    string
	Host           string
	Port           int

	// 上游端点。默认为生产地址，测试与联调时可直接覆盖。
	DeepSeekBaseURL  string
	BochaBaseURL     string
	Context7Endpoint string
	GrepAppEndpoint  string
}

// Default 返回默认配置。
func Default() Config {
	return Config{
		Host:             DefaultHost,
		Port:             DefaultPort,
		DeepSeekBaseURL:  DefaultDeepSeekBaseURL,
		BochaBaseURL:     DefaultBochaBaseURL,
		Context7Endpoint: DefaultContext7Endpoint,
		GrepAppEndpoint:  DefaultGrepAppEndpoint,
	}
}

type fileConfig struct {
	DeepSeekAPIKey *string `yaml:"deepseek_api_key"`
	BochaAPIKey    *string `yaml:"bocha_api_key"`
	Host           *string `yaml:"host"`
	Port           *int    `yaml:"port"`
}

// Load 按优先级合并配置：启动参数 > 环境变量 > 配置文件 > 默认值。
// 只有显式传入 --config 时才读取配置文件。
func Load(args []string, stderr io.Writer) (Config, error) {
	fs := flag.NewFlagSet("agent-utils-cn", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		configPath     string
		deepseekAPIKey string
		bochaAPIKey    string
		host           string
		port           int
	)
	fs.StringVar(&configPath, "config", "", "YAML 配置文件路径")
	fs.StringVar(&deepseekAPIKey, "deepseek-api-key", "", "DeepSeek API Key")
	fs.StringVar(&bochaAPIKey, "bocha-api-key", "", "博查 API Key")
	fs.StringVar(&host, "host", "", "监听地址")
	fs.IntVar(&port, "port", 0, "监听端口")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("未知参数: %s", strings.Join(fs.Args(), " "))
	}

	setByFlag := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { setByFlag[f.Name] = true })

	cfg := Default()

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return Config{}, fmt.Errorf("读取配置文件失败: %w", err)
		}
		var fc fileConfig
		if err := yaml.Unmarshal(data, &fc); err != nil {
			return Config{}, fmt.Errorf("解析配置文件失败: %w", err)
		}
		if fc.DeepSeekAPIKey != nil {
			cfg.DeepSeekAPIKey = *fc.DeepSeekAPIKey
		}
		if fc.BochaAPIKey != nil {
			cfg.BochaAPIKey = *fc.BochaAPIKey
		}
		if fc.Host != nil {
			cfg.Host = *fc.Host
		}
		if fc.Port != nil {
			cfg.Port = *fc.Port
		}
	}

	if v, ok := os.LookupEnv("DEEPSEEK_API_KEY"); ok {
		cfg.DeepSeekAPIKey = v
	}
	if v, ok := os.LookupEnv("BOCHA_API_KEY"); ok {
		cfg.BochaAPIKey = v
	}
	if v, ok := os.LookupEnv("AGENT_UTILS_HOST"); ok {
		cfg.Host = v
	}
	if v, ok := os.LookupEnv("AGENT_UTILS_PORT"); ok && strings.TrimSpace(v) != "" {
		p, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return Config{}, fmt.Errorf("环境变量 AGENT_UTILS_PORT 不是合法端口: %q", v)
		}
		cfg.Port = p
	}

	if setByFlag["deepseek-api-key"] {
		cfg.DeepSeekAPIKey = deepseekAPIKey
	}
	if setByFlag["bocha-api-key"] {
		cfg.BochaAPIKey = bochaAPIKey
	}
	if setByFlag["host"] {
		cfg.Host = host
	}
	if setByFlag["port"] {
		cfg.Port = port
	}

	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("端口必须在 1-65535 之间，当前为 %d", cfg.Port)
	}
	return cfg, nil
}

// IsHelp 判断错误是否表示用户请求了帮助信息。
func IsHelp(err error) bool {
	return errors.Is(err, flag.ErrHelp)
}
