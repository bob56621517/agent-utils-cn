// Package upstream 封装固定工具使用的上游 HTTP 接口，以及失败判定。
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sxyr/agent-utils-cn/internal/provider"
)

// DefaultTimeout 是单次上游请求的超时时间。
const DefaultTimeout = 30 * time.Second

// maxResponseBytes 限制单次响应读取量，避免异常响应耗尽内存。
const maxResponseBytes = 64 << 20

// Response 是一次上游 HTTP 调用的结果。非 2xx 也通过 Response 返回，
// 由调用方决定是否兜底。
type Response struct {
	StatusCode int
	Body       []byte
	Message    string
}

// OK 表示上游返回了 2xx。
func (r *Response) OK() bool {
	return r.StatusCode >= 200 && r.StatusCode < 300
}

// HTTPError 表示上游返回了非 2xx。
type HTTPError struct {
	Provider provider.Name
	Status   int
	Message  string
}

func (e *HTTPError) Error() string {
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, msg)
}

// TransportError 表示请求没有拿到 HTTP 响应（网络错误、超时等）。
type TransportError struct {
	Provider provider.Name
	Err      error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("%s: 请求失败: %v", e.Provider, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

type client struct {
	provider   provider.Name
	baseURL    string
	apiKey     string
	httpClient *http.Client
	timeout    time.Duration
	parseError func([]byte) string
}

func newClient(p provider.Name, baseURL, apiKey string, parseError func([]byte) string) *client {
	return &client{
		provider:   p,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{},
		timeout:    DefaultTimeout,
		parseError: parseError,
	}
}

func (c *client) post(ctx context.Context, path string, payload any) (*Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%s: 构造请求失败: %w", c.provider, err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: 构造请求失败: %w", c.provider, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &TransportError{Provider: c.provider, Err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, &TransportError{Provider: c.provider, Err: err}
	}

	out := &Response{StatusCode: resp.StatusCode, Body: respBody}
	if !out.OK() {
		out.Message = c.parseError(respBody)
	}
	return out, nil
}

// DeepSeek 调用 DeepSeek 的搜索与抓取接口。
type DeepSeek struct {
	*client
}

// NewDeepSeek 构造 DeepSeek 客户端。
func NewDeepSeek(baseURL, apiKey string) *DeepSeek {
	return &DeepSeek{client: newClient(provider.DeepSeek, baseURL, apiKey, deepSeekErrorMessage)}
}

// Search 调用 DeepSeek 搜索接口。兜底路径不会使用额外参数。
func (d *DeepSeek) Search(ctx context.Context, query string, count int) (*Response, error) {
	return d.post(ctx, "/v1/tools/search", struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}{Query: query, Count: count})
}

// Fetch 调用 DeepSeek 抓取接口。
func (d *DeepSeek) Fetch(ctx context.Context, targetURL string) (*Response, error) {
	return d.post(ctx, "/v1/tools/fetch", struct {
		URL string `json:"url"`
	}{URL: targetURL})
}

// Bocha 调用博查的搜索与抓取接口。
type Bocha struct {
	*client
}

// NewBocha 构造博查客户端。
func NewBocha(baseURL, apiKey string) *Bocha {
	return &Bocha{client: newClient(provider.Bocha, baseURL, apiKey, bochaErrorMessage)}
}

type bochaSearchPayload struct {
	Query     string `json:"query"`
	Summary   bool   `json:"summary"`
	Freshness string `json:"freshness,omitempty"`
	Count     int    `json:"count"`
}

// WebSearch 调用博查网页搜索接口。
func (b *Bocha) WebSearch(ctx context.Context, query, freshness string, count int) (*Response, error) {
	return b.post(ctx, "/v1/web-search", bochaSearchPayload{
		Query: query, Summary: true, Freshness: freshness, Count: count,
	})
}

// AISearch 调用博查 AI 搜索接口。
func (b *Bocha) AISearch(ctx context.Context, query, freshness string, count int) (*Response, error) {
	return b.post(ctx, "/v1/ai-search", bochaSearchPayload{
		Query: query, Summary: true, Freshness: freshness, Count: count,
	})
}

// CompatibleSearch 调用博查的 DeepSeek 兼容搜索接口，只透传 query 与 count。
func (b *Bocha) CompatibleSearch(ctx context.Context, query string, count int) (*Response, error) {
	return b.post(ctx, "/v1/deepseek-search", struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}{Query: query, Count: count})
}

// CompatibleFetch 调用博查的 DeepSeek 兼容抓取接口，只透传 url。
func (b *Bocha) CompatibleFetch(ctx context.Context, targetURL string) (*Response, error) {
	return b.post(ctx, "/v1/deepseek-fetch", struct {
		URL string `json:"url"`
	}{URL: targetURL})
}

// ShouldFallback 判断某个供应商返回的状态码是否应触发兜底。
//
// 触发：408、429、5xx、401、402；博查侧 403 表示余额或额度不足，同样触发。
// 不触发：400、403（DeepSeek 侧内容不可得）、404 等其余状态码。
func ShouldFallback(p provider.Name, status int) bool {
	switch status {
	case http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusRequestTimeout,
		http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return p == provider.Bocha
	default:
		return status >= 500
	}
}

func deepSeekErrorMessage(body []byte) string {
	var v struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err == nil && strings.TrimSpace(v.Error.Message) != "" {
		return v.Error.Message
	}
	return truncateBody(body)
}

func bochaErrorMessage(body []byte) string {
	var v struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	}
	if err := json.Unmarshal(body, &v); err == nil && strings.TrimSpace(v.Message) != "" {
		return v.Message
	}
	return truncateBody(body)
}

func truncateBody(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	const limit = 512
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
