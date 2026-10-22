package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sxyr/agent-utils-cn/internal/config"
	"github.com/sxyr/agent-utils-cn/internal/server"
)

const (
	deepSeekSearchPath = "/v1/tools/search"
	deepSeekFetchPath  = "/v1/tools/fetch"
	bochaWebSearchPath = "/v1/web-search"
	bochaAISearchPath  = "/v1/ai-search"
	bochaCompatSearch  = "/v1/deepseek-search"
	bochaCompatFetch   = "/v1/deepseek-fetch"
)

// ---------- 假 REST 上游 ----------

type fakeResponse struct {
	status int
	body   string
}

type recordedRequest struct {
	method string
	path   string
	header http.Header
	body   []byte
}

type fakeHTTP struct {
	server    *httptest.Server
	mu        sync.Mutex
	responses map[string]fakeResponse
	requests  []recordedRequest
}

func newFakeHTTP(t *testing.T) *fakeHTTP {
	t.Helper()
	f := &fakeHTTP{responses: make(map[string]fakeResponse)}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeHTTP) url() string { return f.server.URL }

func (f *fakeHTTP) close() { f.server.Close() }

func (f *fakeHTTP) set(path string, status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[path] = fakeResponse{status: status, body: body}
}

func (f *fakeHTTP) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{
		method: r.Method,
		path:   r.URL.Path,
		header: r.Header.Clone(),
		body:   body,
	})
	resp, ok := f.responses[r.URL.Path]
	f.mu.Unlock()

	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"fake upstream: path not configured"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

func (f *fakeHTTP) calls(path string) []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedRequest
	for _, req := range f.requests {
		if req.path == path {
			out = append(out, req)
		}
	}
	return out
}

func (f *fakeHTTP) callCount(path string) int { return len(f.calls(path)) }

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v (%s)", err, body)
	}
	return out
}

// ---------- 假 MCP 上游 ----------

type fakeMCPCall struct {
	name      string
	arguments json.RawMessage
}

type mcpHandlerFunc func(context.Context, json.RawMessage) (*mcp.CallToolResult, error)

type fakeMCP struct {
	server   *httptest.Server
	mu       sync.Mutex
	calls    []fakeMCPCall
	handlers map[string]mcpHandlerFunc
}

func newFakeMCP(t *testing.T, name string, toolDefs []*mcp.Tool, handlers map[string]mcpHandlerFunc) *fakeMCP {
	t.Helper()
	f := &fakeMCP{handlers: handlers}
	srv := mcp.NewServer(&mcp.Implementation{Name: name, Version: "v0"}, nil)
	for _, tool := range toolDefs {
		tool := tool
		srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.mu.Lock()
			f.calls = append(f.calls, fakeMCPCall{
				name:      req.Params.Name,
				arguments: append(json.RawMessage(nil), req.Params.Arguments...),
			})
			f.mu.Unlock()
			if h, ok := f.handlers[tool.Name]; ok {
				return h(ctx, req.Params.Arguments)
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
			}, nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	f.server = httptest.NewServer(handler)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeMCP) endpoint() string { return f.server.URL + "/mcp" }

func (f *fakeMCP) close() { f.server.Close() }

func (f *fakeMCP) recorded() []fakeMCPCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeMCPCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func testTool(name, description string, schema map[string]any) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, InputSchema: schema}
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// ---------- 被测服务 ----------

type serviceOptions struct {
	deepseekKey      string
	bochaKey         string
	context7Tools    []*mcp.Tool
	context7Handlers map[string]mcpHandlerFunc
	grepTools        []*mcp.Tool
	grepHandlers     map[string]mcpHandlerFunc
	context7Down     bool
	grepDown         bool
}

type testService struct {
	ds       *fakeHTTP
	bocha    *fakeHTTP
	context7 *fakeMCP
	grepApp  *fakeMCP
	srv      *server.Server
	http     *httptest.Server
}

func newTestService(t *testing.T, opts serviceOptions) *testService {
	t.Helper()
	ds := newFakeHTTP(t)
	bocha := newFakeHTTP(t)
	context7 := newFakeMCP(t, "context7", opts.context7Tools, opts.context7Handlers)
	grepApp := newFakeMCP(t, "grep_app", opts.grepTools, opts.grepHandlers)

	cfg := config.Default()
	cfg.DeepSeekAPIKey = opts.deepseekKey
	cfg.BochaAPIKey = opts.bochaKey
	cfg.DeepSeekBaseURL = ds.url()
	cfg.BochaBaseURL = bocha.url()
	cfg.Context7Endpoint = context7.endpoint()
	cfg.GrepAppEndpoint = grepApp.endpoint()
	if opts.context7Down {
		cfg.Context7Endpoint = closedEndpoint(t)
	}
	if opts.grepDown {
		cfg.GrepAppEndpoint = closedEndpoint(t)
	}

	srv, err := server.New(context.Background(), server.Options{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(hs.Close)

	return &testService{
		ds:       ds,
		bocha:    bocha,
		context7: context7,
		grepApp:  grepApp,
		srv:      srv,
		http:     hs,
	}
}

func closedEndpoint(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(http.NotFoundHandler())
	endpoint := ts.URL + "/mcp"
	ts.Close()
	return endpoint
}

func (s *testService) connect(t *testing.T, query string) (*mcp.ClientSession, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             s.http.URL + "/mcp" + query,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		cancel()
		t.Fatalf("连接服务失败: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		cancel()
	})
	return session, ctx
}

func (s *testService) listTools(t *testing.T, query string) map[string]*mcp.Tool {
	t.Helper()
	session, ctx := s.connect(t, query)
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list 失败: %v", err)
	}
	out := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}

func (s *testService) callTool(t *testing.T, query, name string, args any) (*mcp.CallToolResult, error) {
	t.Helper()
	session, ctx := s.connect(t, query)
	return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

func toolNames(tools map[string]*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func assertToolNames(t *testing.T, got map[string]*mcp.Tool, want []string) {
	t.Helper()
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	gotNames := toolNames(got)
	if len(gotNames) != len(sorted) {
		t.Fatalf("可见工具不符\n got: %v\nwant: %v", gotNames, sorted)
	}
	for i := range gotNames {
		if gotNames[i] != sorted[i] {
			t.Fatalf("可见工具不符\n got: %v\nwant: %v", gotNames, sorted)
		}
	}
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("调用结果为空")
	}
	if len(res.Content) != 1 {
		t.Fatalf("期望一条 content，实际 %d 条", len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("期望 text content，实际 %T", res.Content[0])
	}
	return text.Text
}

func sourceOf(res *mcp.CallToolResult) string {
	meta := res.GetMeta()
	if meta == nil {
		return ""
	}
	source, _ := meta["source"].(string)
	return source
}

func normalizeJSON(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	return out
}

func decodeJSONText(t *testing.T, text string) any {
	t.Helper()
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("内容不是合法 JSON: %v (%s)", err, text)
	}
	return out
}

func containsAll(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("结果中缺少 %q：%s", want, text)
		}
	}
}

func containsNone(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if strings.Contains(text, want) {
			t.Fatalf("结果中不应出现 %q：%s", want, text)
		}
	}
}
