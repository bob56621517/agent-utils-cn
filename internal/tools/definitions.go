package tools

import (
	"github.com/sxyr/agent-utils-cn/internal/provider"
)

// 本文件集中定义四个固定工具的名称、描述与参数 schema。
//
// 描述文本由项目负责人维护：需要调整工具用途说明时只改这里，
// 不要把描述散落到处理逻辑或别处。

const (
	descBochaSearch   = "使用博查网页搜索接口检索互联网网页，返回网页标题、链接与摘要。它不是 ds_search 的回退项，而是与之并列的补充搜索来源：适合需要交叉验证或 DeepSeek 结果不足时使用；成本相对较高，但双来源更稳定。"
	descBochaAISearch = "使用博查 AI 搜索接口，返回带总结与来源网页的答案。适合需要综合回答的问题。"
	descDSSearch      = "使用 DeepSeek 搜索接口检索互联网内容。优先使用 DeepSeek；DeepSeek 失败时自动回退到博查（与 bocha_search 相同的上游能力）。适合通用网络检索。"
	descDSFetch       = "抓取指定 URL 的网页内容并转换为 Markdown。优先使用内置的浏览器渲染工具，本工具是回退项，仅在内置工具不可用时使用；上游优先 DeepSeek，失败时自动回退到博查。"

	queryDescription     = "搜索关键词，建议不超过 128 个字符。"
	countDescription     = "返回结果数量，取值范围 1-50，默认 10。事实类查询建议保持默认 10 条；仅长尾冷门问题才适当增加，其余情况尽量控制在 20 条以内。"
	freshnessDescription = "时间范围过滤，默认 noLimit。"
	urlDescription       = "要抓取的网页 URL。"
)

var freshnessValues = []string{"noLimit", "oneDay", "oneWeek", "oneMonth", "oneYear"}

func searchSchema(withFreshness bool) map[string]any {
	props := map[string]any{
		"query": map[string]any{
			"type":        "string",
			"description": queryDescription,
		},
		"count": map[string]any{
			"type":        "integer",
			"description": countDescription,
			"minimum":     1,
			"maximum":     50,
			"default":     10,
		},
	}
	if withFreshness {
		props["freshness"] = map[string]any{
			"type":        "string",
			"description": freshnessDescription,
			"enum":        freshnessValues,
			"default":     "noLimit",
		}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func fetchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": urlDescription,
			},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

type fixedSpec struct {
	name        string
	description string
	schema      map[string]any
	providers   []provider.Name
	call        func(deps Deps, available map[provider.Name]bool) CallFunc
}

func fixedSpecs() []fixedSpec {
	return []fixedSpec{
		{
			name:        "bocha_search",
			description: descBochaSearch,
			schema:      searchSchema(true),
			providers:   []provider.Name{provider.Bocha},
			call:        bochaSearchCall,
		},
		{
			name:        "bocha_ai_search",
			description: descBochaAISearch,
			schema:      searchSchema(true),
			providers:   []provider.Name{provider.Bocha},
			call:        bochaAISearchCall,
		},
		{
			name:        "ds_search",
			description: descDSSearch,
			schema:      searchSchema(false),
			providers:   []provider.Name{provider.DeepSeek, provider.Bocha},
			call:        dsSearchCall,
		},
		{
			name:        "ds_fetch",
			description: descDSFetch,
			schema:      fetchSchema(),
			providers:   []provider.Name{provider.DeepSeek, provider.Bocha},
			call:        dsFetchCall,
		},
	}
}

// FixedSupport 返回四个固定工具的名称与支撑供应商，用于启动期登记与冲突检测。
func FixedSupport() map[string][]provider.Name {
	out := make(map[string][]provider.Name)
	for _, spec := range fixedSpecs() {
		out[spec.name] = append([]provider.Name(nil), spec.providers...)
	}
	return out
}

// Fixed 按本次请求可用的供应商构造固定工具。
func Fixed(deps Deps, available map[provider.Name]bool) []Tool {
	specs := fixedSpecs()
	out := make([]Tool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, Tool{
			Name:        spec.name,
			Description: spec.description,
			InputSchema: spec.schema,
			Providers:   append([]provider.Name(nil), spec.providers...),
			Call:        spec.call(deps, available),
		})
	}
	return out
}
