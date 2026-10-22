// Package filter 解析请求上的 includes / excludes，并计算本次请求可见的工具与可用供应商。
package filter

import (
	"net/url"
	"strings"

	"github.com/sxyr/agent-utils-cn/internal/provider"
)

// Selection 是一次请求的过滤结果。
type Selection struct {
	// Providers 是本次请求可用的供应商（已应用 excludes）。
	Providers map[provider.Name]bool
	// Tools 是本次请求可见的工具名。
	Tools map[string]bool
}

// ParseList 读取 query 中的过滤参数。
// 同时支持重复参数（includes=a&includes=b）与逗号分隔（includes=a,b）。
func ParseList(q url.Values, key string) []string {
	var out []string
	for _, raw := range q[key] {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// Select 计算一次请求的可见工具。
//
// supports 为工具名到支撑供应商的映射。规则：
//   - 供应商可用 = 基础可用（凭据已配置或上游可达）且未被 excludes 排除；
//   - 工具可见的前提是至少一个支撑它的供应商可用；
//   - includes 为空时纳入全部工具；非空时只保留被直接点名的工具，
//     以及被点名供应商所支撑的工具；
//   - excludes 优先于 includes：点名供应商会把它从可用集合中移除，
//     点名工具会直接移除该工具；
//   - 精确匹配、区分大小写，未知名字静默忽略。
func Select(base map[provider.Name]bool, supports map[string][]provider.Name, includes, excludes []string) Selection {
	effective := make(map[provider.Name]bool, len(base))
	for _, p := range provider.All() {
		if base[p] {
			effective[p] = true
		}
	}
	for _, name := range excludes {
		if p, ok := provider.Parse(name); ok {
			delete(effective, p)
		}
	}

	visible := make(map[string]bool, len(supports))
	for tool, providers := range supports {
		var supported []provider.Name
		for _, p := range providers {
			if effective[p] {
				supported = append(supported, p)
			}
		}
		if len(supported) == 0 {
			continue
		}
		if len(includes) == 0 {
			visible[tool] = true
			continue
		}
		for _, name := range includes {
			if name == tool {
				visible[tool] = true
				break
			}
			if p, ok := provider.Parse(name); ok && contains(supported, p) {
				visible[tool] = true
				break
			}
		}
	}

	for _, name := range excludes {
		if _, isProvider := provider.Parse(name); isProvider {
			continue
		}
		// 点工具名直接移除；未知名字在这里自然无效。
		delete(visible, name)
	}

	return Selection{Providers: effective, Tools: visible}
}

func contains(names []provider.Name, want provider.Name) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
