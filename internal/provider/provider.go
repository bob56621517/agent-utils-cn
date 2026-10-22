// Package provider 定义上游供应商，以及供应商在过滤参数中使用的名字。
package provider

// Name 是供应商的逻辑名字，同时也是过滤参数中的组名。
type Name string

const (
	DeepSeek Name = "deepseek"
	Bocha    Name = "bocha"
	Context7 Name = "context7"
	GrepApp  Name = "grep_app"
)

var all = []Name{DeepSeek, Bocha, Context7, GrepApp}

// All 返回全部供应商。
func All() []Name {
	out := make([]Name, len(all))
	copy(out, all)
	return out
}

// Parse 按精确名字匹配供应商。匹配区分大小写，未知名字返回 false。
func Parse(name string) (Name, bool) {
	for _, p := range all {
		if string(p) == name {
			return p, true
		}
	}
	return "", false
}
