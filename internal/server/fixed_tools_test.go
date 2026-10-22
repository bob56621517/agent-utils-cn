package server_test

import (
	"encoding/json"
	"strconv"
	"testing"
)

func newFixedService(t *testing.T) *testService {
	t.Helper()
	return newTestService(t, serviceOptions{
		deepseekKey: "ds-key",
		bochaKey:    "bocha-key",
	})
}

func assertAuth(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("Authorization 头不符: %q", got)
	}
}

func TestDeepSeekSearchSuccess(t *testing.T) {
	svc := newFixedService(t)
	body := `{"results":[{"title":"go","url":"https://go.dev","snippet":"..."}]}`
	svc.ds.set(deepSeekSearchPath, 200, body)

	res, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "golang", "count": 5})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文应原样透传\n got: %s\nwant: %s", got, body)
	}
	if got := sourceOf(res); got != "deepseek" {
		t.Fatalf("source 应为 deepseek，实际 %q", got)
	}

	calls := svc.ds.calls(deepSeekSearchPath)
	if len(calls) != 1 {
		t.Fatalf("deepseek 调用次数不符: %d", len(calls))
	}
	assertAuth(t, calls[0].header.Get("Authorization"), "Bearer ds-key")
	payload := decodeBody(t, calls[0].body)
	if len(payload) != 2 || payload["query"] != "golang" || payload["count"] != float64(5) {
		t.Fatalf("请求体不符: %#v", payload)
	}
	if svc.bocha.callCount(bochaCompatSearch) != 0 {
		t.Fatal("主供应商成功时不应调用博查")
	}
}

func TestDeepSeekSearchDefaultCount(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekSearchPath, 200, `{"results":[]}`)

	if _, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "q"}); err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	payload := decodeBody(t, svc.ds.calls(deepSeekSearchPath)[0].body)
	if payload["count"] != float64(10) {
		t.Fatalf("默认 count 应为 10，实际 %#v", payload["count"])
	}
}

func TestDeepSeekSearchFallbackStatuses(t *testing.T) {
	for _, status := range []int{401, 402, 408, 429, 500, 502, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			svc := newFixedService(t)
			svc.ds.set(deepSeekSearchPath, status, `{"error":{"message":"primary boom"}}`)
			body := `{"results":[{"title":"from bocha"}]}`
			svc.bocha.set(bochaCompatSearch, 200, body)

			res, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "q", "count": 7})
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if res.IsError {
				t.Fatalf("兜底成功后不应是错误: %s", resultText(t, res))
			}
			if got := resultText(t, res); got != body {
				t.Fatalf("正文应原样透传: %s", got)
			}
			if got := sourceOf(res); got != "bocha" {
				t.Fatalf("source 应为 bocha，实际 %q", got)
			}
			if got := svc.ds.callCount(deepSeekSearchPath); got != 1 {
				t.Fatalf("deepseek 应只调用一次，实际 %d", got)
			}
			calls := svc.bocha.calls(bochaCompatSearch)
			if len(calls) != 1 {
				t.Fatalf("博查兜底应调用一次，实际 %d", len(calls))
			}
			assertAuth(t, calls[0].header.Get("Authorization"), "Bearer bocha-key")
			payload := decodeBody(t, calls[0].body)
			if len(payload) != 2 || payload["query"] != "q" || payload["count"] != float64(7) {
				t.Fatalf("兜底只应透传 query 与 count: %#v", payload)
			}
		})
	}
}

func TestDeepSeekSearchNonFallbackStatuses(t *testing.T) {
	for _, status := range []int{400, 403, 404} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			svc := newFixedService(t)
			svc.ds.set(deepSeekSearchPath, status, `{"error":{"message":"primary boom"}}`)
			svc.bocha.set(bochaCompatSearch, 200, `{"results":[]}`)

			res, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "q"})
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if !res.IsError {
				t.Fatal("应返回 isError")
			}
			containsAll(t, resultText(t, res), "deepseek", strconv.Itoa(status), "primary boom")
			if got := svc.bocha.callCount(bochaCompatSearch); got != 0 {
				t.Fatalf("不应触发兜底，实际调用博查 %d 次", got)
			}
		})
	}
}

func TestDeepSeekSearchTransportErrorFallsBack(t *testing.T) {
	svc := newFixedService(t)
	body := `{"results":[{"title":"from bocha"}]}`
	svc.bocha.set(bochaCompatSearch, 200, body)
	svc.ds.close()

	res, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("网络错误后兜底应成功: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文不符: %s", got)
	}
	if got := sourceOf(res); got != "bocha" {
		t.Fatalf("source 应为 bocha，实际 %q", got)
	}
}

func TestDeepSeekSearchBothFail(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekSearchPath, 500, `{"error":{"message":"primary boom"}}`)
	svc.bocha.set(bochaCompatSearch, 503, `{"message":"fallback boom","code":"502"}`)

	res, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !res.IsError {
		t.Fatal("双侧失败应返回 isError")
	}
	containsAll(t, resultText(t, res), "deepseek", "500", "primary boom", "bocha", "503", "fallback boom")
}

func TestExcludedPrimarySkipsDeepSeek(t *testing.T) {
	svc := newFixedService(t)
	svc.bocha.set(bochaCompatSearch, 200, `{"results":[{"title":"from bocha"}]}`)

	res, err := svc.callTool(t, "?excludes=deepseek", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", resultText(t, res))
	}
	if got := sourceOf(res); got != "bocha" {
		t.Fatalf("source 应为 bocha，实际 %q", got)
	}
	if got := svc.ds.callCount(deepSeekSearchPath); got != 0 {
		t.Fatalf("deepseek 被排除后不应被调用，实际 %d 次", got)
	}
}

func TestExcludedPrimaryFailureOnlyReportsBocha(t *testing.T) {
	svc := newFixedService(t)
	svc.bocha.set(bochaCompatSearch, 500, `{"message":"fallback boom"}`)

	res, err := svc.callTool(t, "?excludes=deepseek", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !res.IsError {
		t.Fatal("应返回 isError")
	}
	containsAll(t, resultText(t, res), "bocha", "500", "fallback boom")
	containsNone(t, resultText(t, res), "deepseek")
}

func TestExcludedFallbackStaysOnDeepSeek(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekSearchPath, 200, `{"results":[]}`)

	res, err := svc.callTool(t, "?excludes=bocha", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", resultText(t, res))
	}
	if got := sourceOf(res); got != "deepseek" {
		t.Fatalf("source 应为 deepseek，实际 %q", got)
	}
	if got := svc.bocha.callCount(bochaCompatSearch); got != 0 {
		t.Fatalf("博查被排除后不应被调用，实际 %d 次", got)
	}
}

func TestExcludedFallbackFailureDoesNotFallBack(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekSearchPath, 500, `{"error":{"message":"primary boom"}}`)

	res, err := svc.callTool(t, "?excludes=bocha", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !res.IsError {
		t.Fatal("应返回 isError")
	}
	containsAll(t, resultText(t, res), "deepseek", "500", "primary boom")
	if got := svc.bocha.callCount(bochaCompatSearch); got != 0 {
		t.Fatalf("博查被排除后不应被调用，实际 %d 次", got)
	}
}

func TestEmptyResultsDoNotFallBack(t *testing.T) {
	svc := newFixedService(t)
	body := `{"results":[]}`
	svc.ds.set(deepSeekSearchPath, 200, body)
	svc.bocha.set(bochaCompatSearch, 200, `{"results":[{"title":"from bocha"}]}`)

	res, err := svc.callTool(t, "", "ds_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("空结果不应是错误: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文不符: %s", got)
	}
	if got := sourceOf(res); got != "deepseek" {
		t.Fatalf("source 应为 deepseek，实际 %q", got)
	}
	if got := svc.bocha.callCount(bochaCompatSearch); got != 0 {
		t.Fatalf("空结果不应触发兜底，实际调用博查 %d 次", got)
	}
}

func TestDeepSeekFetchSuccess(t *testing.T) {
	svc := newFixedService(t)
	body := `{"title":"Example","content":"# Hello"}`
	svc.ds.set(deepSeekFetchPath, 200, body)

	res, err := svc.callTool(t, "", "ds_fetch", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文应原样透传: %s", got)
	}
	if got := sourceOf(res); got != "deepseek" {
		t.Fatalf("source 应为 deepseek，实际 %q", got)
	}
	payload := decodeBody(t, svc.ds.calls(deepSeekFetchPath)[0].body)
	if len(payload) != 1 || payload["url"] != "https://example.com" {
		t.Fatalf("请求体不符: %#v", payload)
	}
}

func TestDeepSeekFetchFallback(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekFetchPath, 500, `{"error":{"message":"primary boom"}}`)
	body := `{"title":"From Bocha","content":"# Hi"}`
	svc.bocha.set(bochaCompatFetch, 200, body)

	res, err := svc.callTool(t, "", "ds_fetch", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("兜底成功后不应是错误: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文应原样透传: %s", got)
	}
	if got := sourceOf(res); got != "bocha" {
		t.Fatalf("source 应为 bocha，实际 %q", got)
	}
	calls := svc.bocha.calls(bochaCompatFetch)
	if len(calls) != 1 {
		t.Fatalf("博查兜底应调用一次，实际 %d", len(calls))
	}
	payload := decodeBody(t, calls[0].body)
	if len(payload) != 1 || payload["url"] != "https://example.com" {
		t.Fatalf("兜底只应透传 url: %#v", payload)
	}
}

func TestDeepSeekFetchForbiddenDoesNotFallBack(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekFetchPath, 403, `{"error":{"message":"content unavailable"}}`)

	res, err := svc.callTool(t, "", "ds_fetch", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !res.IsError {
		t.Fatal("应返回 isError")
	}
	containsAll(t, resultText(t, res), "deepseek", "403", "content unavailable")
	if got := svc.bocha.callCount(bochaCompatFetch); got != 0 {
		t.Fatalf("403 不应触发兜底，实际调用博查 %d 次", got)
	}
}

func TestBochaSearch(t *testing.T) {
	svc := newFixedService(t)
	body := `{"data":{"webPages":{"value":[{"name":"Example","url":"https://example.com"}]}}}`
	svc.bocha.set(bochaWebSearchPath, 200, body)

	res, err := svc.callTool(t, "", "bocha_search", map[string]any{"query": "q", "count": 3})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文应原样透传: %s", got)
	}
	if got := sourceOf(res); got != "" {
		t.Fatalf("bocha_search 不应写入 source，实际 %q", got)
	}
	calls := svc.bocha.calls(bochaWebSearchPath)
	if len(calls) != 1 {
		t.Fatalf("调用次数不符: %d", len(calls))
	}
	assertAuth(t, calls[0].header.Get("Authorization"), "Bearer bocha-key")
	payload := decodeBody(t, calls[0].body)
	if payload["query"] != "q" || payload["count"] != float64(3) ||
		payload["summary"] != true || payload["freshness"] != "noLimit" {
		t.Fatalf("请求体不符: %#v", payload)
	}
}

func TestBochaAISearch(t *testing.T) {
	svc := newFixedService(t)
	body := `{"messages":[{"content_type":"webpage","content":"{}"}]}`
	svc.bocha.set(bochaAISearchPath, 200, body)

	res, err := svc.callTool(t, "", "bocha_ai_search", map[string]any{"query": "q", "freshness": "oneWeek"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", resultText(t, res))
	}
	if got := resultText(t, res); got != body {
		t.Fatalf("正文应原样透传: %s", got)
	}
	payload := decodeBody(t, svc.bocha.calls(bochaAISearchPath)[0].body)
	if payload["freshness"] != "oneWeek" || payload["summary"] != true || payload["count"] != float64(10) {
		t.Fatalf("请求体不符: %#v", payload)
	}
}

func TestBochaErrorIsToolErrorWithoutRetry(t *testing.T) {
	svc := newFixedService(t)
	svc.bocha.set(bochaWebSearchPath, 502, `{"message":"bocha boom","code":"502"}`)

	res, err := svc.callTool(t, "", "bocha_search", map[string]any{"query": "q"})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !res.IsError {
		t.Fatal("应返回 isError")
	}
	containsAll(t, resultText(t, res), "bocha", "502", "bocha boom")
	if got := svc.bocha.callCount(bochaWebSearchPath); got != 1 {
		t.Fatalf("不做额外重试，实际调用 %d 次", got)
	}
}

func TestFixedToolInputValidation(t *testing.T) {
	svc := newFixedService(t)

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"搜索缺少 query", "ds_search", map[string]any{}},
		{"count 超范围", "ds_search", map[string]any{"query": "q", "count": 100}},
		{"抓取缺少 url", "ds_fetch", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.callTool(t, "", tc.tool, tc.args)
			if err != nil {
				t.Fatalf("调用失败: %v", err)
			}
			if !res.IsError {
				t.Fatal("参数错误应返回 isError")
			}
		})
	}

	if got := len(svc.ds.calls(deepSeekSearchPath)) + len(svc.ds.calls(deepSeekFetchPath)) +
		len(svc.bocha.calls(bochaCompatSearch)) + len(svc.bocha.calls(bochaCompatFetch)); got != 0 {
		t.Fatalf("参数校验失败时不应访问上游，实际 %d 次", got)
	}
}

func TestCallToolArgumentsAreForwardedRaw(t *testing.T) {
	svc := newFixedService(t)
	svc.ds.set(deepSeekSearchPath, 200, `{"results":[]}`)

	_, err := svc.callTool(t, "", "ds_search", json.RawMessage(`{"query":"q","count":2}`))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	payload := decodeBody(t, svc.ds.calls(deepSeekSearchPath)[0].body)
	if payload["query"] != "q" || payload["count"] != float64(2) {
		t.Fatalf("请求体不符: %#v", payload)
	}
}
