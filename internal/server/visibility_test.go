package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sxyr/agent-utils-cn/internal/config"
	"github.com/sxyr/agent-utils-cn/internal/server"
)

var (
	allFixedTools = []string{"bocha_ai_search", "bocha_search", "ds_fetch", "ds_search"}
	allContext7   = []string{"query-docs", "resolve-library-id"}
	allGrepApp    = []string{"searchGitHub"}
	allTools      = []string{"bocha_ai_search", "bocha_search", "ds_fetch", "ds_search", "query-docs", "resolve-library-id", "searchGitHub"}
)

func standardContext7Tools() []*mcp.Tool {
	return []*mcp.Tool{
		testTool("resolve-library-id", "Resolve a library name to a Context7 ID",
			objectSchema(map[string]any{"libraryName": map[string]any{"type": "string"}}, "libraryName")),
		testTool("query-docs", "Query documentation for a library",
			objectSchema(map[string]any{
				"libraryId": map[string]any{"type": "string"},
				"query":     map[string]any{"type": "string"},
			}, "libraryId", "query")),
	}
}

func standardGrepAppTools() []*mcp.Tool {
	return []*mcp.Tool{
		testTool("searchGitHub", "Search GitHub code",
			objectSchema(map[string]any{"q": map[string]any{"type": "string"}}, "q")),
	}
}

func TestToolVisibility(t *testing.T) {
	svc := newTestService(t, serviceOptions{
		deepseekKey:   "ds-key",
		bochaKey:      "bocha-key",
		context7Tools: standardContext7Tools(),
		grepTools:     standardGrepAppTools(),
	})

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"不带过滤参数时全部可见", "", allTools},
		{"includes 点名供应商 bocha", "?includes=bocha", allFixedTools},
		{"includes 点名供应商 context7", "?includes=context7", allContext7},
		{"includes 混合供应商与工具", "?includes=context7,ds_fetch", []string{"ds_fetch", "query-docs", "resolve-library-id"}},
		{"includes 点名供应商 grep_app", "?includes=grep_app", allGrepApp},
		{"includes 直接点名工具", "?includes=ds_fetch", []string{"ds_fetch"}},
		{"excludes 只屏蔽 deepseek 时 ds_* 仍可见", "?excludes=deepseek", allTools},
		{"excludes 屏蔽 bocha 时保留 ds_*", "?excludes=bocha", []string{"ds_fetch", "ds_search", "query-docs", "resolve-library-id", "searchGitHub"}},
		{"excludes 同时屏蔽两个来源后 ds_* 消失", "?excludes=deepseek,bocha", []string{"query-docs", "resolve-library-id", "searchGitHub"}},
		{"excludes 优先于 includes", "?includes=bocha&excludes=ds_search", []string{"bocha_ai_search", "bocha_search", "ds_fetch"}},
		{"excludes 移除单个工具", "?excludes=bocha_search", []string{"bocha_ai_search", "ds_fetch", "ds_search", "query-docs", "resolve-library-id", "searchGitHub"}},
		{"excludes 移除透传工具", "?includes=context7&excludes=query-docs", []string{"resolve-library-id"}},
		{"重复参数与逗号分隔等价", "?includes=context7&excludes=query-docs&includes=ds_fetch&excludes=ds_search", []string{"ds_fetch", "resolve-library-id"}},
		{"大小写敏感", "?includes=BOCHA", nil},
		{"未知名字静默忽略", "?includes=unknown", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertToolNames(t, svc.listTools(t, tc.query), tc.want)
		})
	}
}

func TestToolVisibilityWithoutCredentials(t *testing.T) {
	cases := []struct {
		name string
		opts serviceOptions
		want []string
	}{
		{
			name: "没有任何凭据",
			opts: serviceOptions{},
			want: nil,
		},
		{
			name: "只有 deepseek 凭据",
			opts: serviceOptions{deepseekKey: "ds-key"},
			want: []string{"ds_fetch", "ds_search"},
		},
		{
			name: "只有 bocha 凭据",
			opts: serviceOptions{bochaKey: "bocha-key"},
			want: allFixedTools,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newTestService(t, tc.opts)
			assertToolNames(t, svc.listTools(t, ""), tc.want)
		})
	}

	t.Run("只有 deepseek 凭据时 includes=bocha 不引入工具", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{deepseekKey: "ds-key"})
		assertToolNames(t, svc.listTools(t, "?includes=bocha"), nil)
	})

	t.Run("只有 deepseek 凭据时可排除唯一来源", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{deepseekKey: "ds-key"})
		assertToolNames(t, svc.listTools(t, "?excludes=deepseek"), nil)
	})
}

func TestPassThroughTools(t *testing.T) {
	t.Run("名称描述与 schema 原样暴露", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{
			context7Tools: standardContext7Tools(),
			grepTools:     standardGrepAppTools(),
		})
		tools := svc.listTools(t, "")

		resolve, ok := tools["resolve-library-id"]
		if !ok {
			t.Fatal("缺少 resolve-library-id")
		}
		if resolve.Description != "Resolve a library name to a Context7 ID" {
			t.Fatalf("描述没有原样透传: %q", resolve.Description)
		}
		wantSchema := objectSchema(map[string]any{"libraryName": map[string]any{"type": "string"}}, "libraryName")
		if got, want := normalizeJSON(t, resolve.InputSchema), normalizeJSON(t, wantSchema); !reflect.DeepEqual(got, want) {
			t.Fatalf("schema 没有原样透传\n got: %#v\nwant: %#v", got, want)
		}

		searchGithub, ok := tools["searchGitHub"]
		if !ok {
			t.Fatal("缺少 searchGitHub")
		}
		if searchGithub.Description != "Search GitHub code" {
			t.Fatalf("描述没有原样透传: %q", searchGithub.Description)
		}
	})

	t.Run("调用时名称与参数原样转发", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{
			context7Tools: standardContext7Tools(),
			context7Handlers: map[string]mcpHandlerFunc{
				"resolve-library-id": func(_ context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{
						Content: []mcp.Content{&mcp.TextContent{Text: string(args)}},
					}, nil
				},
			},
		})
		res, err := svc.callTool(t, "", "resolve-library-id", map[string]any{"libraryName": "react"})
		if err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if res.IsError {
			t.Fatalf("不应是错误结果: %s", resultText(t, res))
		}
		if got := decodeJSONText(t, resultText(t, res)); !reflect.DeepEqual(got, map[string]any{"libraryName": "react"}) {
			t.Fatalf("返回内容不符: %#v", got)
		}
		calls := svc.context7.recorded()
		if len(calls) != 1 || calls[0].name != "resolve-library-id" {
			t.Fatalf("上游收到的调用不符: %#v", calls)
		}
		if got := normalizeJSON(t, calls[0].arguments); !reflect.DeepEqual(got, map[string]any{"libraryName": "react"}) {
			t.Fatalf("上游收到的参数不符: %#v", got)
		}
	})

	t.Run("上游 isError 结果原样透传", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{
			grepTools: standardGrepAppTools(),
			grepHandlers: map[string]mcpHandlerFunc{
				"searchGitHub": func(context.Context, json.RawMessage) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{
						IsError: true,
						Content: []mcp.Content{&mcp.TextContent{Text: "upstream failed"}},
					}, nil
				},
			},
		})
		res, err := svc.callTool(t, "", "searchGitHub", map[string]any{"q": "mcp"})
		if err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if !res.IsError {
			t.Fatal("应保留上游的 isError")
		}
		if got := resultText(t, res); got != "upstream failed" {
			t.Fatalf("错误内容不符: %q", got)
		}
	})

	t.Run("上游启动时不可达则该供应商降级", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{
			bochaKey:      "bocha-key",
			context7Tools: standardContext7Tools(),
			context7Down:  true,
			grepTools:     standardGrepAppTools(),
		})
		assertToolNames(t, svc.listTools(t, ""), []string{
			"bocha_ai_search", "bocha_search", "ds_fetch", "ds_search", "searchGitHub",
		})
	})

	t.Run("上游在启动后才不可达时调用返回 isError", func(t *testing.T) {
		svc := newTestService(t, serviceOptions{
			context7Tools: standardContext7Tools(),
		})
		svc.context7.close()
		res, err := svc.callTool(t, "", "query-docs", map[string]any{"libraryId": "/react", "query": "hooks"})
		if err != nil {
			t.Fatalf("调用失败: %v", err)
		}
		if !res.IsError {
			t.Fatal("上游不可达时应返回 isError")
		}
		containsAll(t, resultText(t, res), "context7")
	})
}

func TestProtocolVersion(t *testing.T) {
	svc := newTestService(t, serviceOptions{})
	session, _ := svc.connect(t, "")
	got := session.InitializeResult()
	if got == nil || got.ProtocolVersion != "2026-07-28" {
		t.Fatalf("期望协商到 2026-07-28，实际 %#v", got)
	}
}

func TestHealth(t *testing.T) {
	svc := newTestService(t, serviceOptions{})
	for _, path := range []string{"/health", "/healthz"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(svc.http.URL + path)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("状态码不符: %d", resp.StatusCode)
			}
			body, _ := io.ReadAll(resp.Body)
			want := `{"service":"agent-utils-cn","status":"ok"}`
			if string(body) != want {
				t.Fatalf("响应不符: %s", body)
			}
		})
	}
}

func TestStartupToolNameConflict(t *testing.T) {
	newServer := func(t *testing.T, c7Tools, grepTools []*mcp.Tool) error {
		t.Helper()
		context7 := newFakeMCP(t, "context7", c7Tools, nil)
		grepApp := newFakeMCP(t, "grep_app", grepTools, nil)
		cfg := config.Default()
		cfg.Context7Endpoint = context7.endpoint()
		cfg.GrepAppEndpoint = grepApp.endpoint()
		_, err := server.New(context.Background(), server.Options{
			Config: cfg,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		return err
	}

	t.Run("固定工具与透传工具冲突", func(t *testing.T) {
		err := newServer(t, []*mcp.Tool{testTool("ds_search", "conflict", objectSchema(nil))}, nil)
		if err == nil {
			t.Fatal("期望启动失败")
		}
		containsAll(t, err.Error(), "ds_search", "固定工具", "context7")
	})

	t.Run("两个透传供应商之间冲突", func(t *testing.T) {
		err := newServer(t,
			[]*mcp.Tool{testTool("searchGitHub", "a", objectSchema(nil))},
			[]*mcp.Tool{testTool("searchGitHub", "b", objectSchema(nil))},
		)
		if err == nil {
			t.Fatal("期望启动失败")
		}
		containsAll(t, err.Error(), "searchGitHub", "context7", "grep_app")
	})
}

func TestNoResourcesOrPrompts(t *testing.T) {
	svc := newTestService(t, serviceOptions{})
	session, ctx := svc.connect(t, "")
	resources, err := session.ListResources(ctx, nil)
	if err == nil && len(resources.Resources) > 0 {
		t.Fatalf("不应暴露 resources: %#v", resources.Resources)
	}
	prompts, err := session.ListPrompts(ctx, nil)
	if err == nil && len(prompts.Prompts) > 0 {
		t.Fatalf("不应暴露 prompts: %#v", prompts.Prompts)
	}
	caps := session.InitializeResult().Capabilities
	if caps != nil {
		if caps.Resources != nil {
			t.Fatalf("不应声明 resources 能力: %#v", caps.Resources)
		}
		if caps.Prompts != nil {
			t.Fatalf("不应声明 prompts 能力: %#v", caps.Prompts)
		}
	}
}

func TestQueryParsingIsPerRequest(t *testing.T) {
	svc := newTestService(t, serviceOptions{deepseekKey: "ds-key", bochaKey: "bocha-key"})
	assertToolNames(t, svc.listTools(t, "?includes=ds_search"), []string{"ds_search"})
	assertToolNames(t, svc.listTools(t, "?includes=bocha_search"), []string{"bocha_search"})
	assertToolNames(t, svc.listTools(t, ""), allFixedTools)

	// 同一个客户端会话内，工具面在连接时已经确定；不同 URL 的连接互不影响。
	sessionA, ctxA := svc.connect(t, "?includes=bocha")
	listA, err := sessionA.ListTools(ctxA, nil)
	if err != nil {
		t.Fatalf("tools/list 失败: %v", err)
	}
	if len(listA.Tools) != 4 {
		t.Fatalf("会话 A 应看到四个固定工具，实际 %d", len(listA.Tools))
	}
}
