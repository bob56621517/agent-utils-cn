// Package tools 定义对外暴露的工具：四个固定工具与透传工具的统一载体。
package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sxyr/agent-utils-cn/internal/provider"
)

// CallFunc 是工具的实际处理逻辑。
type CallFunc func(ctx context.Context, args json.RawMessage) (*mcp.CallToolResult, error)

// Tool 是本服务内部对 MCP 工具的完整描述。
type Tool struct {
	Name         string
	Description  string
	InputSchema  any
	OutputSchema any
	Annotations  *mcp.ToolAnnotations
	Meta         mcp.Meta
	// Providers 是支撑该工具的供应商集合。
	Providers []provider.Name
	Call      CallFunc
}

// MCPTool 转换为 SDK 的工具描述。调用方负责注册处理器。
func (t Tool) MCPTool() *mcp.Tool {
	return &mcp.Tool{
		Name:         t.Name,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		OutputSchema: t.OutputSchema,
		Annotations:  t.Annotations,
		Meta:         t.Meta,
	}
}

// ErrorResult 构造协议内的工具错误结果（isError=true）。
func ErrorResult(err error) *mcp.CallToolResult {
	var res mcp.CallToolResult
	res.SetError(err)
	return &res
}

// textResult 构造单条 text content 的成功结果。
// source 非空时写入 _meta.source，用于标注实际供应商。
func textResult(body []byte, source provider.Name) *mcp.CallToolResult {
	res := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}
	if source != "" {
		res.Meta = mcp.Meta{"source": string(source)}
	}
	return res
}
