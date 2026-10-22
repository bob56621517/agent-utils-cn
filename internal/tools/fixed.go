package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sxyr/agent-utils-cn/internal/provider"
	"github.com/sxyr/agent-utils-cn/internal/upstream"
)

// Deps 是固定工具依赖的上游客户端。
type Deps struct {
	DeepSeek *upstream.DeepSeek
	Bocha    *upstream.Bocha
}

const defaultSearchCount = 10

type searchArgs struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	Freshness string `json:"freshness"`
}

func parseSearchArgs(raw json.RawMessage) (searchArgs, error) {
	var args searchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return args, fmt.Errorf("参数格式错误: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return args, errors.New("参数 query 不能为空")
	}
	if args.Count == 0 {
		args.Count = defaultSearchCount
	}
	if args.Count < 1 || args.Count > 50 {
		return args, errors.New("参数 count 必须在 1-50 之间")
	}
	if args.Freshness == "" {
		args.Freshness = "noLimit"
	}
	return args, nil
}

type fetchArgs struct {
	URL string `json:"url"`
}

func parseFetchArgs(raw json.RawMessage) (fetchArgs, error) {
	var args fetchArgs
	if err := decodeArgs(raw, &args); err != nil {
		return args, fmt.Errorf("参数格式错误: %w", err)
	}
	if strings.TrimSpace(args.URL) == "" {
		return args, errors.New("参数 url 不能为空")
	}
	return args, nil
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func bochaSearchCall(deps Deps, available map[provider.Name]bool) CallFunc {
	return func(ctx context.Context, raw json.RawMessage) (*mcp.CallToolResult, error) {
		args, err := parseSearchArgs(raw)
		if err != nil {
			return ErrorResult(err), nil
		}
		if !available[provider.Bocha] || deps.Bocha == nil {
			return ErrorResult(errors.New("没有可用的供应商支撑此工具")), nil
		}
		return runAttempts(ctx, false,
			attempt{provider: provider.Bocha, call: func(ctx context.Context) (*upstream.Response, error) {
				return deps.Bocha.WebSearch(ctx, args.Query, args.Freshness, args.Count)
			}},
		), nil
	}
}

func bochaAISearchCall(deps Deps, available map[provider.Name]bool) CallFunc {
	return func(ctx context.Context, raw json.RawMessage) (*mcp.CallToolResult, error) {
		args, err := parseSearchArgs(raw)
		if err != nil {
			return ErrorResult(err), nil
		}
		if !available[provider.Bocha] || deps.Bocha == nil {
			return ErrorResult(errors.New("没有可用的供应商支撑此工具")), nil
		}
		return runAttempts(ctx, false,
			attempt{provider: provider.Bocha, call: func(ctx context.Context) (*upstream.Response, error) {
				return deps.Bocha.AISearch(ctx, args.Query, args.Freshness, args.Count)
			}},
		), nil
	}
}

func dsSearchCall(deps Deps, available map[provider.Name]bool) CallFunc {
	return func(ctx context.Context, raw json.RawMessage) (*mcp.CallToolResult, error) {
		args, err := parseSearchArgs(raw)
		if err != nil {
			return ErrorResult(err), nil
		}
		var attempts []attempt
		if available[provider.DeepSeek] && deps.DeepSeek != nil {
			attempts = append(attempts, attempt{
				provider: provider.DeepSeek,
				call: func(ctx context.Context) (*upstream.Response, error) {
					return deps.DeepSeek.Search(ctx, args.Query, args.Count)
				},
			})
		}
		if available[provider.Bocha] && deps.Bocha != nil {
			attempts = append(attempts, attempt{
				provider: provider.Bocha,
				call: func(ctx context.Context) (*upstream.Response, error) {
					return deps.Bocha.CompatibleSearch(ctx, args.Query, args.Count)
				},
			})
		}
		if len(attempts) == 0 {
			return ErrorResult(errors.New("没有可用的供应商支撑此工具")), nil
		}
		return runAttempts(ctx, true, attempts...), nil
	}
}

func dsFetchCall(deps Deps, available map[provider.Name]bool) CallFunc {
	return func(ctx context.Context, raw json.RawMessage) (*mcp.CallToolResult, error) {
		args, err := parseFetchArgs(raw)
		if err != nil {
			return ErrorResult(err), nil
		}
		var attempts []attempt
		if available[provider.DeepSeek] && deps.DeepSeek != nil {
			attempts = append(attempts, attempt{
				provider: provider.DeepSeek,
				call: func(ctx context.Context) (*upstream.Response, error) {
					return deps.DeepSeek.Fetch(ctx, args.URL)
				},
			})
		}
		if available[provider.Bocha] && deps.Bocha != nil {
			attempts = append(attempts, attempt{
				provider: provider.Bocha,
				call: func(ctx context.Context) (*upstream.Response, error) {
					return deps.Bocha.CompatibleFetch(ctx, args.URL)
				},
			})
		}
		if len(attempts) == 0 {
			return ErrorResult(errors.New("没有可用的供应商支撑此工具")), nil
		}
		return runAttempts(ctx, true, attempts...), nil
	}
}

type attempt struct {
	provider provider.Name
	call     func(context.Context) (*upstream.Response, error)
}

// runAttempts 按顺序调用供应商，只在失败可兜底时继续下一个。
// annotate 为 true 时在成功结果中写入 _meta.source。
func runAttempts(ctx context.Context, annotate bool, attempts ...attempt) *mcp.CallToolResult {
	var failures []error
	for i, a := range attempts {
		resp, err := a.call(ctx)
		if err == nil && resp == nil {
			err = fmt.Errorf("%s: 上游返回空响应", a.provider)
		}
		if err == nil && !resp.OK() {
			err = &upstream.HTTPError{
				Provider: a.provider,
				Status:   resp.StatusCode,
				Message:  resp.Message,
			}
		}
		if err == nil {
			source := provider.Name("")
			if annotate {
				source = a.provider
			}
			return textResult(resp.Body, source)
		}
		failures = append(failures, err)
		if i == len(attempts)-1 || !eligibleForFallback(a.provider, err) {
			break
		}
	}
	if len(failures) == 1 {
		return ErrorResult(failures[0])
	}
	parts := make([]string, len(failures))
	for i, err := range failures {
		parts[i] = err.Error()
	}
	return ErrorResult(errors.New(strings.Join(parts, "; ")))
}

func eligibleForFallback(p provider.Name, err error) bool {
	var httpErr *upstream.HTTPError
	if errors.As(err, &httpErr) {
		return upstream.ShouldFallback(p, httpErr.Status)
	}
	var transportErr *upstream.TransportError
	if errors.As(err, &transportErr) {
		return true
	}
	// 空响应等异常按网络故障处理。
	return true
}
