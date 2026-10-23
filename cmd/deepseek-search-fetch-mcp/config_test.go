package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	unsetEnv(t, "DEEPSEEK_API_KEY")
	unsetEnv(t, "DEEPSEEK_API_URL")
	unsetEnv(t, "DEEPSEEK_SEARCH_FETCH_HOST")
	unsetEnv(t, "DEEPSEEK_SEARCH_FETCH_PORT")
	return dir
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("清除环境变量 %s 失败: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
		}
	})
}

func configFilePath(home string) string {
	return filepath.Join(home, dataDirName, configFileName)
}

func writeConfigFile(t *testing.T, home, content string) {
	t.Helper()
	path := configFilePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFromFixedConfigFile(t *testing.T) {
	home := isolateHome(t)
	writeConfigFile(t, home, `{
		"deepseek_api_key": "file-key",
		"deepseek_api_url": "https://example.com",
		"server_host": "0.0.0.0",
		"server_port": 18090,
		"request_timeout": "45s",
		"http_max_idle_conns": 8,
		"http_max_idle_conns_per_host": 4,
		"http_idle_conn_timeout": "60s",
		"http_tls_handshake_timeout": "5s",
		"reset_connection_on_error": false
	}`)

	cfg, err := loadConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.apiKey != "file-key" || cfg.apiURL != "https://example.com" || cfg.host != "0.0.0.0" || cfg.port != 18090 {
		t.Fatalf("配置文件未正确读取: %#v", cfg)
	}
	if cfg.requestTimeout != 45*time.Second ||
		cfg.httpMaxIdleConns != 8 ||
		cfg.httpMaxIdleConnsPerHost != 4 ||
		cfg.httpIdleConnTimeout != 60*time.Second ||
		cfg.httpTLSHandshakeTimeout != 5*time.Second ||
		cfg.resetConnectionOnError {
		t.Fatalf("HTTP 调优参数未正确读取: %#v", cfg)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	home := isolateHome(t)
	writeConfigFile(t, home, `{
		"deepseek_api_key": "file-key",
		"deepseek_api_url": "https://file.example.com",
		"server_host": "127.0.0.1",
		"server_port": 18090
	}`)
	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	t.Setenv("DEEPSEEK_API_URL", "https://env.example.com")
	t.Setenv("DEEPSEEK_SEARCH_FETCH_HOST", "0.0.0.0")
	t.Setenv("DEEPSEEK_SEARCH_FETCH_PORT", "18091")

	cfg, err := loadConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.apiKey != "env-key" || cfg.apiURL != "https://env.example.com" || cfg.host != "0.0.0.0" || cfg.port != 18091 {
		t.Fatalf("环境变量应覆盖配置文件: %#v", cfg)
	}
}

func TestFlagsOverrideEnv(t *testing.T) {
	isolateHome(t)
	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	t.Setenv("DEEPSEEK_API_URL", "https://env.example.com")

	cfg, err := loadConfig([]string{
		"--deepseek-api-key=flag-key",
		"--deepseek-api-url=https://flag.example.com",
		"--host=0.0.0.0",
		"--port=9999",
	}, io.Discard)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.apiKey != "flag-key" || cfg.apiURL != "https://flag.example.com" || cfg.host != "0.0.0.0" || cfg.port != 9999 {
		t.Fatalf("启动参数应优先: %#v", cfg)
	}
}

func TestFirstRunCreatesConfigFile(t *testing.T) {
	home := isolateHome(t)

	_, err := loadConfig(nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "未配置 DeepSeek API Key") {
		t.Fatalf("缺少 API Key 时应报错: %v", err)
	}

	data, err := os.ReadFile(configFilePath(home))
	if err != nil {
		t.Fatalf("首次启动应创建配置文件: %v", err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("模板不是合法 JSON: %v (%s)", err, data)
	}
	if _, ok := file["deepseek_api_key"]; !ok {
		t.Fatalf("模板缺少 deepseek_api_key: %s", data)
	}
	if file["deepseek_api_url"] != defaultAPIURL {
		t.Fatalf("模板中的 deepseek_api_url 不符: %s", data)
	}
	if file["server_host"] != defaultHost {
		t.Fatalf("模板中的 server_host 不符: %s", data)
	}
	if file["server_port"] != float64(defaultPort) {
		t.Fatalf("模板中的 server_port 不符: %s", data)
	}
	if file["request_timeout"] != durationText(defaultRequestTimeout) {
		t.Fatalf("模板中的 request_timeout 不符: %s", data)
	}
	if file["http_max_idle_conns"] != float64(defaultHTTPMaxIdleConns) {
		t.Fatalf("模板中的 http_max_idle_conns 不符: %s", data)
	}
	if file["http_max_idle_conns_per_host"] != float64(defaultHTTPMaxIdleConnsPerHost) {
		t.Fatalf("模板中的 http_max_idle_conns_per_host 不符: %s", data)
	}
	if file["http_idle_conn_timeout"] != durationText(defaultHTTPIdleConnTimeout) {
		t.Fatalf("模板中的 http_idle_conn_timeout 不符: %s", data)
	}
	if file["http_tls_handshake_timeout"] != durationText(defaultHTTPTLSHandshakeTimeout) {
		t.Fatalf("模板中的 http_tls_handshake_timeout 不符: %s", data)
	}
	if file["reset_connection_on_error"] != defaultResetConnectionOnError {
		t.Fatalf("模板中的 reset_connection_on_error 不符: %s", data)
	}
}

func TestCreatesConfigFileWhenUsingEnv(t *testing.T) {
	home := isolateHome(t)
	t.Setenv("DEEPSEEK_API_KEY", "env-key")

	cfg, err := loadConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.apiKey != "env-key" {
		t.Fatalf("API Key 不符: %q", cfg.apiKey)
	}
	if _, err := os.Stat(configFilePath(home)); err != nil {
		t.Fatalf("使用环境变量时也应创建配置文件: %v", err)
	}
}

func TestMalformedConfigFile(t *testing.T) {
	home := isolateHome(t)
	writeConfigFile(t, home, `{"deepseek_api_key":`)

	_, err := loadConfig(nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "解析配置文件") {
		t.Fatalf("配置格式错误时应报错: %v", err)
	}
}

func TestConfigFileCreatedBeforeValidation(t *testing.T) {
	home := isolateHome(t)

	_, err := loadConfig([]string{"--port=70000"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "1-65535") {
		t.Fatalf("端口越界时应报错: %v", err)
	}
	if _, err := os.Stat(configFilePath(home)); err != nil {
		t.Fatalf("参数校验失败前也应创建配置文件: %v", err)
	}
}

func TestInvalidPort(t *testing.T) {
	isolateHome(t)
	t.Setenv("DEEPSEEK_API_KEY", "env-key")

	_, err := loadConfig([]string{"--port=70000"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "1-65535") {
		t.Fatalf("端口越界时应报错: %v", err)
	}
}

func TestInvalidDuration(t *testing.T) {
	home := isolateHome(t)
	writeConfigFile(t, home, `{"deepseek_api_key":"k","request_timeout":"abc"}`)

	_, err := loadConfig(nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "request_timeout") {
		t.Fatalf("非法时长应报错: %v", err)
	}
}

func TestInvalidHTTPTuning(t *testing.T) {
	home := isolateHome(t)
	writeConfigFile(t, home, `{"deepseek_api_key":"k","http_max_idle_conns":0}`)

	_, err := loadConfig(nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "http_max_idle_conns") {
		t.Fatalf("非法连接池配置应报错: %v", err)
	}
}

func TestInvalidAPIURL(t *testing.T) {
	isolateHome(t)
	t.Setenv("DEEPSEEK_API_KEY", "env-key")

	_, err := loadConfig([]string{"--deepseek-api-url=not-a-url"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "API URL 不合法") {
		t.Fatalf("API URL 非法时应报错: %v", err)
	}
}
