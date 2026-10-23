package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// defaultBaseURL 是 DeepSeek 官方 API 地址。
	defaultBaseURL = "https://api.deepseek.com"
	// maxResponseSize 限制单次响应读取量，避免异常响应耗尽内存。
	maxResponseSize = 64 << 20
)

// Options 是上游 HTTP 客户端的调优参数。
type Options struct {
	// Timeout 用于 Search 等常规请求。
	Timeout time.Duration
	// FetchTimeout 用于抓取请求。抓取需要浏览器渲染，耗时明显高于搜索。
	FetchTimeout        time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
	// ResetOnError 为 true 时，任何上游调用失败都会丢弃空闲连接，
	// 避免复用处于异常状态的连接（尤其是认证、计费等错误之后）。
	ResetOnError bool
}

// DefaultOptions 返回默认的客户端调优参数。
func DefaultOptions() Options {
	return Options{
		Timeout:             30 * time.Second,
		FetchTimeout:        60 * time.Second,
		MaxIdleConns:        5,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ResetOnError:        true,
	}
}

// Client 调用 DeepSeek 官方 Search 与 Fetch 接口。
type Client struct {
	baseURL      string
	apiKey       string
	http         *http.Client
	transport    *http.Transport
	timeout      time.Duration
	fetchTimeout time.Duration
	resetOnError bool
}

// NewClient 构造上游客户端。
func NewClient(baseURL, apiKey string, opts Options) *Client {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	transport := newTransport(opts)
	fetchTimeout := opts.FetchTimeout
	if fetchTimeout <= 0 {
		fetchTimeout = opts.Timeout
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		http:         &http.Client{Transport: transport},
		transport:    transport,
		timeout:      opts.Timeout,
		fetchTimeout: fetchTimeout,
		resetOnError: opts.ResetOnError,
	}
}

func newTransport(opts Options) *http.Transport {
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment}
	}
	transport.MaxIdleConns = opts.MaxIdleConns
	transport.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	transport.IdleConnTimeout = opts.IdleConnTimeout
	transport.TLSHandshakeTimeout = opts.TLSHandshakeTimeout
	transport.ForceAttemptHTTP2 = true
	return transport
}

// CloseIdleConnections 丢弃当前所有空闲连接，下次调用会重新建连。
func (c *Client) CloseIdleConnections() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

func (c *Client) resetIdleConnections() {
	if c.resetOnError {
		c.CloseIdleConnections()
	}
}

// SearchRequest 是 /v1/tools/search 的请求体。
// 搜索结果不返回正文，正文统一通过 Fetch 获取。
type SearchRequest struct {
	Query        string  `json:"query"`
	Count        int     `json:"count,omitempty"`
	OriQuestion  *string `json:"ori_question,omitempty"`
	Localization string  `json:"localization,omitempty"`
	// NeedContent 为 true 时请求上游同时返回网页正文，用于预热本地缓存。
	NeedContent bool `json:"need_content,omitempty"`
}

// Search 调用搜索接口，成功时原样返回上游 JSON。
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]byte, error) {
	return c.post(ctx, "/v1/tools/search", req, c.timeout)
}

// Fetch 调用抓取接口，成功时原样返回上游 JSON。
// 抓取需要浏览器渲染，使用独立的、更长的超时。
func (c *Client) Fetch(ctx context.Context, targetURL string) ([]byte, error) {
	return c.post(ctx, "/v1/tools/fetch", struct {
		URL string `json:"url"`
	}{URL: targetURL}, c.fetchTimeout)
}

func (c *Client) post(ctx context.Context, path string, payload any, timeout time.Duration) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		c.resetIdleConnections()
		return nil, fmt.Errorf("请求 DeepSeek 失败: %w", err)
	}

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	closeErr := resp.Body.Close()

	if readErr != nil {
		c.resetIdleConnections()
		return nil, fmt.Errorf("读取 DeepSeek 响应失败: %w", readErr)
	}
	if int64(len(data)) > maxResponseSize {
		c.resetIdleConnections()
		return nil, fmt.Errorf("DeepSeek 响应超过 %d 字节上限", maxResponseSize)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 认证、计费以及其他任何上游失败都丢弃空闲连接，防止复用异常连接。
		c.resetIdleConnections()
		return nil, apiError(resp.StatusCode, data)
	}
	if closeErr != nil {
		c.resetIdleConnections()
	}
	return data, nil
}

// APIError 表示 DeepSeek 返回的非 2xx 响应。
type APIError struct {
	StatusCode int
	Message    string
	Type       string
	Code       string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "DeepSeek 返回 HTTP %d", e.StatusCode)
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	if e.Code != "" && e.Code != e.Type {
		fmt.Fprintf(&b, "（code=%s）", e.Code)
	}
	return b.String()
}

func apiError(status int, body []byte) *APIError {
	e := &APIError{StatusCode: status}
	var v struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err == nil && (v.Error.Message != "" || v.Error.Code != "") {
		e.Message = strings.TrimSpace(v.Error.Message)
		e.Type = v.Error.Type
		e.Code = v.Error.Code
	} else {
		e.Message = truncate(body)
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

func truncate(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	const limit = 512
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
