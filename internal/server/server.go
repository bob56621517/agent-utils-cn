// Package server 负责装配 MCP 服务、按请求过滤工具面，并提供健康检查。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sxyr/agent-utils-cn/internal/config"
	"github.com/sxyr/agent-utils-cn/internal/filter"
	"github.com/sxyr/agent-utils-cn/internal/provider"
	"github.com/sxyr/agent-utils-cn/internal/tools"
	"github.com/sxyr/agent-utils-cn/internal/upstream"
	"github.com/sxyr/agent-utils-cn/internal/version"
)

const serviceName = "agent-utils-cn"

// Options 是构造服务的参数。
type Options struct {
	Config config.Config
	Logger *slog.Logger
}

// Server 持有启动期确定的工具目录与上游客户端；每次 MCP 请求据此生成临时服务端。
type Server struct {
	logger           *slog.Logger
	deps             tools.Deps
	baseAvailability map[provider.Name]bool
	supports         map[string][]provider.Name
	passThrough      []tools.Tool
	handler          http.Handler
}

type fetchResult struct {
	name  provider.Name
	tools []*mcp.Tool
	err   error
}

// New 启动服务：拉取透传供应商的工具列表，登记固定工具，检查名称冲突。
// 单个透传上游不可达时只记录日志并标记该供应商不可用，不影响启动。
func New(ctx context.Context, opts Options) (*Server, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cfg := opts.Config

	s := &Server{
		logger: logger,
		deps: tools.Deps{
			DeepSeek: upstream.NewDeepSeek(cfg.DeepSeekBaseURL, cfg.DeepSeekAPIKey),
			Bocha:    upstream.NewBocha(cfg.BochaBaseURL, cfg.BochaAPIKey),
		},
		baseAvailability: map[provider.Name]bool{
			provider.DeepSeek: cfg.DeepSeekAPIKey != "",
			provider.Bocha:    cfg.BochaAPIKey != "",
		},
		supports: tools.FixedSupport(),
	}

	entries := []struct {
		name     provider.Name
		endpoint string
	}{
		{name: provider.Context7, endpoint: cfg.Context7Endpoint},
		{name: provider.GrepApp, endpoint: cfg.GrepAppEndpoint},
	}
	upstreams := make(map[provider.Name]*provider.MCPUpstream, len(entries))
	for _, entry := range entries {
		upstreams[entry.name] = &provider.MCPUpstream{
			Name:     entry.name,
			Endpoint: entry.endpoint,
			Version:  version.Version,
		}
	}

	results := s.fetchAll(ctx, upstreams)
	sources := make(map[string]string, len(s.supports))
	for name := range s.supports {
		sources[name] = "固定工具"
	}

	var conflicts []string
	for _, result := range results {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.err != nil {
			logger.Warn("透传供应商不可用", "provider", result.name, "err", result.err)
			s.baseAvailability[result.name] = false
			continue
		}
		s.baseAvailability[result.name] = true
		up := upstreams[result.name]
		for _, pt := range result.tools {
			if !usableTool(pt) {
				logger.Warn("忽略不可用的透传工具", "provider", result.name, "tool", pt.Name)
				continue
			}
			source := string(result.name)
			if previous, exists := sources[pt.Name]; exists {
				conflicts = append(conflicts, fmt.Sprintf("%q（%s / %s）", pt.Name, previous, source))
				continue
			}
			sources[pt.Name] = source
			s.supports[pt.Name] = []provider.Name{result.name}
			s.passThrough = append(s.passThrough, passThroughTool(up, pt, result.name))
		}
		logger.Info("已加载透传供应商", "provider", result.name, "tools", len(result.tools))
	}

	if len(conflicts) > 0 {
		return nil, fmt.Errorf("启动失败，工具名冲突: %s", strings.Join(conflicts, "；"))
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return s.buildMCPServer(r.URL.Query())
		},
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
			Logger:       logger,
		},
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/healthz", healthHandler)
	s.handler = mux

	return s, nil
}

// Handler 返回 HTTP 处理器。
func (s *Server) Handler() http.Handler {
	return s.handler
}

// buildMCPServer 为单次请求生成只包含可见工具的临时服务端。
func (s *Server) buildMCPServer(q url.Values) *mcp.Server {
	includes := filter.ParseList(q, "includes")
	excludes := filter.ParseList(q, "excludes")
	selection := filter.Select(s.baseAvailability, s.supports, includes, excludes)

	srv := mcp.NewServer(
		&mcp.Implementation{
			Name:    serviceName,
			Version: version.Version,
		},
		&mcp.ServerOptions{
			// 只暴露 tools；不提供 resources、prompts、logging。
			Capabilities: &mcp.ServerCapabilities{
				Tools: &mcp.ToolCapabilities{},
			},
			Logger: s.logger,
		},
	)

	for _, tool := range tools.Fixed(s.deps, selection.Providers) {
		if !selection.Tools[tool.Name] {
			continue
		}
		srv.AddTool(tool.MCPTool(), registerCall(tool.Call))
	}
	for _, tool := range s.passThrough {
		if !selection.Tools[tool.Name] {
			continue
		}
		srv.AddTool(tool.MCPTool(), registerCall(tool.Call))
	}
	return srv
}

// registerCall 把内部处理器适配为 SDK 的 ToolHandler。
// 参数保持原始 JSON，透传工具因此可以做到参数零改写。
func registerCall(call tools.CallFunc) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return call(ctx, req.Params.Arguments)
	}
}

func (s *Server) fetchAll(ctx context.Context, upstreams map[provider.Name]*provider.MCPUpstream) []fetchResult {
	ch := make(chan fetchResult, len(upstreams))
	var wg sync.WaitGroup
	for name, up := range upstreams {
		name, up := name, up
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetched, err := up.ListTools(ctx)
			ch <- fetchResult{name: name, tools: fetched, err: err}
		}()
	}
	wg.Wait()
	close(ch)

	results := make([]fetchResult, 0, len(upstreams))
	for result := range ch {
		results = append(results, result)
	}
	return results
}

func passThroughTool(up *provider.MCPUpstream, pt *mcp.Tool, owner provider.Name) tools.Tool {
	name := pt.Name
	return tools.Tool{
		Name:         pt.Name,
		Description:  pt.Description,
		InputSchema:  pt.InputSchema,
		OutputSchema: pt.OutputSchema,
		Annotations:  pt.Annotations,
		Meta:         pt.Meta,
		Providers:    []provider.Name{owner},
		Call: func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error) {
			res, err := up.CallTool(ctx, name, args)
			if err != nil {
				return tools.ErrorResult(err), nil
			}
			return res, nil
		},
	}
}

func usableTool(tool *mcp.Tool) bool {
	if tool == nil || tool.Name == "" || len(tool.Name) > 128 {
		return false
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		return false
	}
	typ, _ := schema["type"].(string)
	return typ == "object"
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"service":"agent-utils-cn","status":"ok"}`))
}
