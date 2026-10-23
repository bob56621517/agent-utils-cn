package openapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	defaultSearchCount = 10
	minSearchCount     = 5
	maxSearchCount     = 50
)

// Tool 是 OpenAPI 工具层对外暴露的一项能力。
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Call        func(ctx context.Context, args json.RawMessage) ([]byte, error)
}

// Tools 返回 api/openapi.yaml 声明的全部工具。
func Tools(client *Client) []Tool {
	return []Tool{
		searchTool(client),
		fetchTool(client),
	}
}

func searchTool(client *Client) Tool {
	return Tool{
		Name: "web_ext_search",
		Description: `扩展网页搜索。可以按站点包含/排除、语言、发布时间范围过滤，也支持精确短语和更明确的检索目标。
返回 results 数组。每条结果包含 title、url、snippet、summary、site_name、date，以及两个质量信号：
- rerank_score：0-3 的综合排序分，综合相关性、权威性和时效性；>=1.5 为优质结果。
- authority：0-4 的网页权威性，-1 表示未知；>=3 为极权威结果。
- date：发布时间（UTC，RFC 3339）；未知为 null。`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":      "string",
					"minLength": 1,
					"maxLength": 128,
					"description": `搜索词，1-128 个字符，不能全为空白。支持与关键词组合的搜索语法：
- site:example.com：限定站点；多个站点写作 site:a OR site:b。
- -site:example.com：排除指定站点。
- lang:zh,en：限定语言，多个用逗号分隔。
- after:2025 或 after:2025-01-01：限定发布时间之后。
- before:2026-06-01：限定发布时间之前。
- "精确短语"：精确匹配；-"排除短语"：排除该短语。
- 关键词前加减号：排除包含该关键词的网页。
示例：DeepSeek V4 site:github.com OR site:huggingface.co lang:zh after:2025`,
				},
				"count": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"maximum":     maxSearchCount,
					"default":     defaultSearchCount,
					"description": "返回结果条数，默认 10，最少 5，最多 50。简单事实 5；一般 10；长尾冷门 25。subagent 或独立子任务可适当增大。",
				},
				"summary": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "默认 false；开启后，会返回更长的摘要信息，建议降低 count 的数量。",
				},
				"ori_question": map[string]any{
					"type":        []any{"string", "null"},
					"minLength":   1,
					"maxLength":   128,
					"description": "触发本次搜索的用户原始问题，1-128 个字符；用于帮助理解搜索意图。传 null 视为未提供。",
				},
				"localization": map[string]any{
					"type":        "string",
					"default":     "",
					"description": "用于优先返回本地化搜索结果的 IP 地址，例如 1.2.3.4；不提供时使用服务端默认。",
				},
			},
			"required": []string{"query"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) ([]byte, error) {
			args, err := parseSearchArgs(raw)
			if err != nil {
				return nil, err
			}
			body, err := client.Search(ctx, SearchRequest{
				Query:        args.Query,
				Count:        args.Count,
				OriQuestion:  args.OriQuestion,
				Localization: args.Localization,
			})
			if err != nil {
				return nil, err
			}
			return projectSearchResults(body, args.Summary)
		},
	}
}

func fetchTool(client *Client) Tool {
	return Tool{
		Name: "web_ext_fetch",
		Description: `扩展网页抓取。使用浏览器渲染读取页面，适合 JavaScript 渲染、单页应用和动态加载内容的网页。
返回 title 与 content：title 为页面标题，缺失时为空字符串；content 为 Markdown 正文。`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"minLength":   1,
					"maxLength":   8192,
					"format":      "uri",
					"description": "要抓取的网页 URL，必须是包含主机名的 HTTP/HTTPS 地址，不能携带用户名或密码；长度不超过 8192 字符。",
				},
			},
			"required": []string{"url"},
		},
		Call: func(ctx context.Context, raw json.RawMessage) ([]byte, error) {
			args, err := parseFetchArgs(raw)
			if err != nil {
				return nil, err
			}
			return client.Fetch(ctx, args.URL)
		},
	}
}

type searchArgs struct {
	Query        string  `json:"query"`
	Count        int     `json:"count"`
	Summary      bool    `json:"summary"`
	OriQuestion  *string `json:"ori_question"`
	Localization string  `json:"localization"`
}

func parseSearchArgs(raw json.RawMessage) (searchArgs, error) {
	var args searchArgs
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return args, errors.New("参数 query 不能为空")
	}
	if utf8.RuneCountInString(args.Query) > 128 {
		return args, errors.New("参数 query 不能超过 128 个字符")
	}
	if args.Count == 0 {
		args.Count = defaultSearchCount
	}
	if args.Count < minSearchCount {
		args.Count = minSearchCount
	}
	if args.Count > maxSearchCount {
		return args, fmt.Errorf("参数 count 不能超过 %d", maxSearchCount)
	}
	if args.OriQuestion != nil {
		if strings.TrimSpace(*args.OriQuestion) == "" {
			return args, errors.New("参数 ori_question 不能为空白")
		}
		if utf8.RuneCountInString(*args.OriQuestion) > 128 {
			return args, errors.New("参数 ori_question 不能超过 128 个字符")
		}
	}
	return args, nil
}

type fetchArgs struct {
	URL string `json:"url"`
}

func parseFetchArgs(raw json.RawMessage) (fetchArgs, error) {
	var args fetchArgs
	if err := decode(raw, &args); err != nil {
		return args, err
	}
	if strings.TrimSpace(args.URL) == "" {
		return args, errors.New("参数 url 不能为空")
	}
	if utf8.RuneCountInString(args.URL) > 8192 {
		return args, errors.New("参数 url 不能超过 8192 个字符")
	}
	u, err := url.Parse(args.URL)
	if err != nil {
		return args, fmt.Errorf("参数 url 不合法: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return args, errors.New("参数 url 必须是 HTTP/HTTPS 地址")
	}
	if u.Host == "" {
		return args, errors.New("参数 url 必须包含主机名")
	}
	if u.User != nil {
		return args, errors.New("参数 url 不能携带用户名或密码")
	}
	return args, nil
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet"`
	Summary     string  `json:"summary"`
	SiteName    *string `json:"site_name"`
	Date        *string `json:"date"`
	RerankScore float64 `json:"rerank_score"`
	Authority   int     `json:"authority"`
}

// projectSearchResults 按 summary 参数裁剪结果：
// snippet 与 summary 始终存在，但同一条结果中只有一个非空。
func projectSearchResults(body []byte, summary bool) ([]byte, error) {
	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}
	if resp.Results == nil {
		resp.Results = []searchResult{}
	}
	for i := range resp.Results {
		if summary {
			resp.Results[i].Snippet = ""
		} else {
			resp.Results[i].Summary = ""
		}
	}
	return json.Marshal(resp)
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("参数格式错误: %w", err)
	}
	return nil
}
