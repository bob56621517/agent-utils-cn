package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type capturedRequest struct {
	path   string
	header http.Header
	body   []byte
}

type fakeUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	status int
	body   string
	reqs   []capturedRequest
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{status: http.StatusOK, body: `{}`}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, capturedRequest{path: r.URL.Path, header: r.Header.Clone(), body: body})
		status, respBody := f.status, f.body
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeUpstream) respond(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
	f.body = body
}

func (f *fakeUpstream) calls() []capturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedRequest, len(f.reqs))
	copy(out, f.reqs)
	return out
}

func newTools(t *testing.T) ([]Tool, *fakeUpstream) {
	t.Helper()
	up := newFakeUpstream(t)
	return Tools(NewClient(up.server.URL, "test-key", DefaultOptions())), up
}

func findTool(t *testing.T, all []Tool, name string) Tool {
	t.Helper()
	for _, tool := range all {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("未找到工具 %s", name)
	return Tool{}
}

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v (%s)", err, body)
	}
	return out
}

func TestToolDefinitions(t *testing.T) {
	all, _ := newTools(t)
	if len(all) != 2 {
		t.Fatalf("工具数量应为 2，实际 %d", len(all))
	}
	search := findTool(t, all, "web_ext_search")
	fetch := findTool(t, all, "web_fetch")
	if search.Description == "" || fetch.Description == "" {
		t.Fatal("工具描述不能为空")
	}
	if search.InputSchema["required"].([]string)[0] != "query" {
		t.Fatal("search 必须要求 query")
	}
	if fetch.InputSchema["required"].([]string)[0] != "url" {
		t.Fatal("fetch 必须要求 url")
	}
}

func TestSearchSuccess(t *testing.T) {
	all, up := newTools(t)
	up.respond(200, `{"results":[{"title":"go","url":"https://go.dev","snippet":"short","summary":"long","content":"body","site_icon":"https://icon","site_name":"Go","date":"2026-01-01T00:00:00Z","rerank_score":2.5,"authority":3}]}`)

	body, err := findTool(t, all, "web_ext_search").Call(
		context.Background(),
		json.RawMessage(`{"query":"golang","count":5,"ori_question":"what is go","localization":"1.2.3.4"}`),
	)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("结果不是合法 JSON: %v (%s)", err, body)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("结果数量不符: %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Snippet != "short" || r.Summary != "" {
		t.Fatalf("默认应只返回 snippet: %#v", r)
	}
	if r.RerankScore != 2.5 || r.Authority != 3 {
		t.Fatalf("质量信号未保留: %#v", r)
	}
	if r.SiteName == nil || *r.SiteName != "Go" {
		t.Fatalf("site_name 未保留: %#v", r)
	}
	if r.Date == nil || *r.Date != "2026-01-01T00:00:00Z" {
		t.Fatalf("date 未保留: %#v", r)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	result := raw["results"].([]any)[0].(map[string]any)
	for _, key := range []string{"content", "site_icon"} {
		if _, ok := result[key]; ok {
			t.Fatalf("响应不应包含 %s: %s", key, body)
		}
	}

	calls := up.calls()
	if len(calls) != 1 {
		t.Fatalf("上游调用次数不符: %d", len(calls))
	}
	if calls[0].path != "/v1/tools/search" {
		t.Fatalf("请求路径不符: %s", calls[0].path)
	}
	if got := calls[0].header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("鉴权头不符: %q", got)
	}
	payload := decodeBody(t, calls[0].body)
	if payload["query"] != "golang" || payload["count"] != float64(5) {
		t.Fatalf("请求体不符: %#v", payload)
	}
	if payload["ori_question"] != "what is go" || payload["localization"] != "1.2.3.4" {
		t.Fatalf("可选参数未透传: %#v", payload)
	}
	if _, ok := payload["summary"]; ok {
		t.Fatalf("summary 是包装层参数，不应发往上游: %#v", payload)
	}
}

func TestSearchSummaryMode(t *testing.T) {
	all, up := newTools(t)
	up.respond(200, `{"results":[{"title":"go","url":"https://go.dev","snippet":"short","summary":"long","rerank_score":2.5,"authority":3}]}`)

	body, err := findTool(t, all, "web_ext_search").Call(
		context.Background(),
		json.RawMessage(`{"query":"golang","summary":true}`),
	)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("结果不是合法 JSON: %v (%s)", err, body)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("结果数量不符: %d", len(resp.Results))
	}
	if resp.Results[0].Snippet != "" || resp.Results[0].Summary != "long" {
		t.Fatalf("summary=true 时应只返回 summary: %#v", resp.Results[0])
	}
}

func TestSearchDefaults(t *testing.T) {
	all, up := newTools(t)
	up.respond(200, `{"results":[]}`)

	if _, err := findTool(t, all, "web_ext_search").Call(context.Background(), json.RawMessage(`{"query":"q"}`)); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	payload := decodeBody(t, up.calls()[0].body)
	if payload["count"] != float64(10) {
		t.Fatalf("默认 count 应为 10: %#v", payload)
	}
	for _, key := range []string{"ori_question", "localization", "summary"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("未提供的 %s 不应出现在请求体中: %#v", key, payload)
		}
	}
}

func TestSearchCountClamped(t *testing.T) {
	cases := []struct {
		name string
		args string
		want float64
	}{
		{"过小值按 5 处理", `{"query":"q","count":3}`, 5},
		{"零值使用默认值", `{"query":"q","count":0}`, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			all, up := newTools(t)
			up.respond(200, `{"results":[]}`)

			if _, err := findTool(t, all, "web_ext_search").Call(context.Background(), json.RawMessage(tc.args)); err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			payload := decodeBody(t, up.calls()[0].body)
			if payload["count"] != tc.want {
				t.Fatalf("count 应为 %v，实际 %#v", tc.want, payload["count"])
			}
		})
	}
}

func TestSearchValidation(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"空 query", `{"query":"   "}`},
		{"query 超长", fmt.Sprintf(`{"query":%q}`, strings.Repeat("中", 129))},
		{"count 过大", `{"query":"q","count":51}`},
		{"ori_question 为空白", `{"query":"q","ori_question":"  "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			all, up := newTools(t)
			_, err := findTool(t, all, "web_ext_search").Call(context.Background(), json.RawMessage(tc.args))
			if err == nil {
				t.Fatal("应返回参数错误")
			}
			if len(up.calls()) != 0 {
				t.Fatal("参数校验失败时不应请求上游")
			}
		})
	}
}

func TestSearchAPIError(t *testing.T) {
	all, up := newTools(t)
	up.respond(401, `{"error":{"message":"API Key 缺失或无效","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`)

	_, err := findTool(t, all, "web_ext_search").Call(context.Background(), json.RawMessage(`{"query":"q"}`))
	if err == nil {
		t.Fatal("应返回上游错误")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("错误类型应为 *APIError，实际 %T", err)
	}
	if apiErr.StatusCode != 401 || apiErr.Message != "API Key 缺失或无效" {
		t.Fatalf("错误解析不符: %#v", apiErr)
	}
}

func TestFetchSuccess(t *testing.T) {
	all, up := newTools(t)
	up.respond(200, `{"title":"Example","content":"# Example"}`)

	body, err := findTool(t, all, "web_fetch").Call(
		context.Background(),
		json.RawMessage(`{"url":"https://example.com/article"}`),
	)
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if string(body) != `{"title":"Example","content":"# Example"}` {
		t.Fatalf("响应未原样返回: %s", body)
	}
	calls := up.calls()
	if len(calls) != 1 || calls[0].path != "/v1/tools/fetch" {
		t.Fatalf("上游调用不符: %#v", calls)
	}
	payload := decodeBody(t, calls[0].body)
	if payload["url"] != "https://example.com/article" {
		t.Fatalf("请求体不符: %#v", payload)
	}
}

func TestFetchValidation(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"空 url", `{}`},
		{"非 HTTP", `{"url":"ftp://example.com/a"}`},
		{"缺少主机名", `{"url":"https:///a"}`},
		{"携带凭据", `{"url":"https://user:pass@example.com/a"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			all, up := newTools(t)
			_, err := findTool(t, all, "web_fetch").Call(context.Background(), json.RawMessage(tc.args))
			if err == nil {
				t.Fatal("应返回参数错误")
			}
			if len(up.calls()) != 0 {
				t.Fatal("参数校验失败时不应请求上游")
			}
		})
	}
}
