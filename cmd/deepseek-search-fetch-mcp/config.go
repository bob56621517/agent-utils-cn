package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHost    = "127.0.0.1"
	defaultPort    = 18081
	defaultAPIURL  = "https://api.deepseek.com"
	dataDirName    = ".deepseek_search_fetch"
	configFileName = "config.json"

	defaultRequestTimeout          = 30 * time.Second
	defaultHTTPMaxIdleConns        = 5
	defaultHTTPMaxIdleConnsPerHost = 5
	defaultHTTPIdleConnTimeout     = 90 * time.Second
	defaultHTTPTLSHandshakeTimeout = 10 * time.Second
	defaultResetConnectionOnError  = true
)

type config struct {
	apiKey string
	apiURL string
	host   string
	port   int

	requestTimeout          time.Duration
	httpMaxIdleConns        int
	httpMaxIdleConnsPerHost int
	httpIdleConnTimeout     time.Duration
	httpTLSHandshakeTimeout time.Duration
	resetConnectionOnError  bool
}

type fileConfig struct {
	DeepSeekAPIKey          *string `json:"deepseek_api_key"`
	DeepSeekAPIURL          *string `json:"deepseek_api_url"`
	ServerHost              *string `json:"server_host"`
	ServerPort              *int    `json:"server_port"`
	RequestTimeout          *string `json:"request_timeout"`
	HTTPMaxIdleConns        *int    `json:"http_max_idle_conns"`
	HTTPMaxIdleConnsPerHost *int    `json:"http_max_idle_conns_per_host"`
	HTTPIdleConnTimeout     *string `json:"http_idle_conn_timeout"`
	HTTPTLSHandshakeTimeout *string `json:"http_tls_handshake_timeout"`
	ResetConnectionOnError  *bool   `json:"reset_connection_on_error"`
}

// loadConfig 按优先级读取配置：启动参数 > 环境变量 > 固定配置文件 > 默认值。
// 配置只在启动时读取一次；首次启动时自动创建配置目录与配置文件。
func loadConfig(args []string, stderr io.Writer) (config, error) {
	cfg := config{
		apiURL:                  defaultAPIURL,
		host:                    defaultHost,
		port:                    defaultPort,
		requestTimeout:          defaultRequestTimeout,
		httpMaxIdleConns:        defaultHTTPMaxIdleConns,
		httpMaxIdleConnsPerHost: defaultHTTPMaxIdleConnsPerHost,
		httpIdleConnTimeout:     defaultHTTPIdleConnTimeout,
		httpTLSHandshakeTimeout: defaultHTTPTLSHandshakeTimeout,
		resetConnectionOnError:  defaultResetConnectionOnError,
	}

	fs := flag.NewFlagSet("deepseek-search-fetch-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		apiKey string
		apiURL string
		host   string
		port   int
	)
	fs.StringVar(&apiKey, "deepseek-api-key", "", "DeepSeek API Key")
	fs.StringVar(&apiURL, "deepseek-api-url", "", "DeepSeek API Base URL")
	fs.StringVar(&host, "host", "", "监听地址")
	fs.IntVar(&port, "port", 0, "监听端口")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("未知参数: %s", strings.Join(fs.Args(), " "))
	}
	setByFlag := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { setByFlag[f.Name] = true })

	path, err := ensureConfigFile()
	if err != nil {
		return cfg, err
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
		}
		var file fileConfig
		if err := json.Unmarshal(data, &file); err != nil {
			return cfg, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
		}
		if err := applyFileConfig(&cfg, file); err != nil {
			return cfg, err
		}
	}

	if v, ok := os.LookupEnv("DEEPSEEK_API_KEY"); ok {
		cfg.apiKey = strings.TrimSpace(v)
	}
	if v, ok := os.LookupEnv("DEEPSEEK_API_URL"); ok {
		cfg.apiURL = strings.TrimSpace(v)
	}
	if v, ok := os.LookupEnv("DEEPSEEK_SEARCH_FETCH_HOST"); ok {
		cfg.host = v
	}
	if v, ok := os.LookupEnv("DEEPSEEK_SEARCH_FETCH_PORT"); ok && strings.TrimSpace(v) != "" {
		p, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return cfg, fmt.Errorf("环境变量 DEEPSEEK_SEARCH_FETCH_PORT 不是合法端口: %q", v)
		}
		cfg.port = p
	}

	if setByFlag["deepseek-api-key"] {
		cfg.apiKey = strings.TrimSpace(apiKey)
	}
	if setByFlag["deepseek-api-url"] {
		cfg.apiURL = strings.TrimSpace(apiURL)
	}
	if setByFlag["host"] {
		cfg.host = host
	}
	if setByFlag["port"] {
		cfg.port = port
	}

	if err := validateConfig(&cfg); err != nil {
		return cfg, err
	}
	if cfg.apiKey == "" {
		hint := "请设置 DEEPSEEK_API_KEY 或 --deepseek-api-key"
		if path != "" {
			hint += "，或写入 " + path + " 的 deepseek_api_key"
		}
		return cfg, errors.New("未配置 DeepSeek API Key：" + hint)
	}
	return cfg, nil
}

func applyFileConfig(cfg *config, file fileConfig) error {
	if file.DeepSeekAPIKey != nil {
		cfg.apiKey = strings.TrimSpace(*file.DeepSeekAPIKey)
	}
	if file.DeepSeekAPIURL != nil {
		cfg.apiURL = strings.TrimSpace(*file.DeepSeekAPIURL)
	}
	if file.ServerHost != nil {
		cfg.host = strings.TrimSpace(*file.ServerHost)
	}
	if file.ServerPort != nil {
		cfg.port = *file.ServerPort
	}
	if file.RequestTimeout != nil {
		d, err := parseDuration("request_timeout", *file.RequestTimeout)
		if err != nil {
			return err
		}
		cfg.requestTimeout = d
	}
	if file.HTTPMaxIdleConns != nil {
		cfg.httpMaxIdleConns = *file.HTTPMaxIdleConns
	}
	if file.HTTPMaxIdleConnsPerHost != nil {
		cfg.httpMaxIdleConnsPerHost = *file.HTTPMaxIdleConnsPerHost
	}
	if file.HTTPIdleConnTimeout != nil {
		d, err := parseDuration("http_idle_conn_timeout", *file.HTTPIdleConnTimeout)
		if err != nil {
			return err
		}
		cfg.httpIdleConnTimeout = d
	}
	if file.HTTPTLSHandshakeTimeout != nil {
		d, err := parseDuration("http_tls_handshake_timeout", *file.HTTPTLSHandshakeTimeout)
		if err != nil {
			return err
		}
		cfg.httpTLSHandshakeTimeout = d
	}
	if file.ResetConnectionOnError != nil {
		cfg.resetConnectionOnError = *file.ResetConnectionOnError
	}
	return nil
}

func parseDuration(name, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("配置项 %s 不是合法时长: %q", name, raw)
	}
	return d, nil
}

// durationText 输出更直观的时长文本，例如 90s 而不是 1m30s。
func durationText(d time.Duration) string {
	if d%time.Second == 0 {
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	return d.String()
}

func validateConfig(cfg *config) error {
	if cfg.host == "" {
		cfg.host = defaultHost
	}
	if cfg.port < 1 || cfg.port > 65535 {
		return fmt.Errorf("端口必须在 1-65535 之间，当前为 %d", cfg.port)
	}
	if cfg.apiURL == "" {
		cfg.apiURL = defaultAPIURL
	}
	u, err := url.Parse(cfg.apiURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("DeepSeek API URL 不合法: %q", cfg.apiURL)
	}
	if cfg.requestTimeout <= 0 {
		return fmt.Errorf("request_timeout 必须大于 0，当前为 %s", cfg.requestTimeout)
	}
	if cfg.httpMaxIdleConns < 1 {
		return fmt.Errorf("http_max_idle_conns 必须大于等于 1，当前为 %d", cfg.httpMaxIdleConns)
	}
	if cfg.httpMaxIdleConnsPerHost < 1 {
		return fmt.Errorf("http_max_idle_conns_per_host 必须大于等于 1，当前为 %d", cfg.httpMaxIdleConnsPerHost)
	}
	if cfg.httpIdleConnTimeout <= 0 {
		return fmt.Errorf("http_idle_conn_timeout 必须大于 0，当前为 %s", cfg.httpIdleConnTimeout)
	}
	if cfg.httpTLSHandshakeTimeout <= 0 {
		return fmt.Errorf("http_tls_handshake_timeout 必须大于 0，当前为 %s", cfg.httpTLSHandshakeTimeout)
	}
	return nil
}

// ensureConfigFile 创建配置目录；配置文件不存在时写入模板。
// 无法确定用户主目录时跳过文件，仅使用环境变量或启动参数。
func ensureConfigFile() (string, error) {
	dir := dataDir()
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
	}

	path := filepath.Join(dir, configFileName)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("检查配置文件 %s 失败: %w", path, err)
	}

	content, err := json.MarshalIndent(struct {
		DeepSeekAPIKey          string `json:"deepseek_api_key"`
		DeepSeekAPIURL          string `json:"deepseek_api_url"`
		ServerHost              string `json:"server_host"`
		ServerPort              int    `json:"server_port"`
		RequestTimeout          string `json:"request_timeout"`
		HTTPMaxIdleConns        int    `json:"http_max_idle_conns"`
		HTTPMaxIdleConnsPerHost int    `json:"http_max_idle_conns_per_host"`
		HTTPIdleConnTimeout     string `json:"http_idle_conn_timeout"`
		HTTPTLSHandshakeTimeout string `json:"http_tls_handshake_timeout"`
		ResetConnectionOnError  bool   `json:"reset_connection_on_error"`
	}{
		DeepSeekAPIKey:          "",
		DeepSeekAPIURL:          defaultAPIURL,
		ServerHost:              defaultHost,
		ServerPort:              defaultPort,
		RequestTimeout:          durationText(defaultRequestTimeout),
		HTTPMaxIdleConns:        defaultHTTPMaxIdleConns,
		HTTPMaxIdleConnsPerHost: defaultHTTPMaxIdleConnsPerHost,
		HTTPIdleConnTimeout:     durationText(defaultHTTPIdleConnTimeout),
		HTTPTLSHandshakeTimeout: durationText(defaultHTTPTLSHandshakeTimeout),
		ResetConnectionOnError:  defaultResetConnectionOnError,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("生成配置文件模板失败: %w", err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("创建配置文件 %s 失败: %w", path, err)
	}
	return path, nil
}

func dataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, dataDirName)
}
