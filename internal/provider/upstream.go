package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultMCPTimeout 是透传上游单次连接或调用的超时时间。
const DefaultMCPTimeout = 30 * time.Second

// MCPUpstream 表示一个透传供应商的 MCP 端点。
//
// 本服务以无状态方式工作：拉取工具列表与调用工具都建立临时会话，
// 不跨请求复用连接。
type MCPUpstream struct {
	Name       Name
	Endpoint   string
	Version    string
	HTTPClient *http.Client
	Timeout    time.Duration
}

func (u *MCPUpstream) timeout() time.Duration {
	if u.Timeout > 0 {
		return u.Timeout
	}
	return DefaultMCPTimeout
}

func (u *MCPUpstream) connect(ctx context.Context) (*mcp.ClientSession, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(ctx, u.timeout())
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "agent-utils-cn",
		Version: u.Version,
	}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             u.Endpoint,
		HTTPClient:           u.HTTPClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return session, cancel, nil
}

// ListTools 拉取上游全部工具，自动翻页。
func (u *MCPUpstream) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	session, cancel, err := u.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: 连接上游失败: %w", u.Name, err)
	}
	defer cancel()
	defer session.Close()

	var tools []*mcp.Tool
	params := &mcp.ListToolsParams{}
	for page := 0; ; page++ {
		if page >= 1000 {
			return nil, fmt.Errorf("%s: 工具列表分页超过上限", u.Name)
		}
		res, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("%s: 拉取工具列表失败: %w", u.Name, err)
		}
		tools = append(tools, res.Tools...)
		if res.NextCursor == "" {
			return tools, nil
		}
		params.Cursor = res.NextCursor
	}
}

// CallTool 调用上游工具，名称与参数原样透传。
func (u *MCPUpstream) CallTool(ctx context.Context, name string, args json.RawMessage) (*mcp.CallToolResult, error) {
	session, cancel, err := u.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: 连接上游失败: %w", u.Name, err)
	}
	defer cancel()
	defer session.Close()

	var arguments any = args
	if len(args) == 0 {
		arguments = map[string]any{}
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: 调用工具 %s 失败: %w", u.Name, name, err)
	}
	return res, nil
}
